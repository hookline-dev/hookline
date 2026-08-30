package worker

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"hookline/internal/backoff"
	"hookline/internal/breaker"
	"hookline/internal/delivery"
	"hookline/internal/domain"
	"hookline/internal/queue"
)

type RateLimiter interface {
	Allow(context.Context, domain.EndpointID, time.Time) (bool, time.Time, error)
}
type (
	Metrics interface{ ObserveDelivery(delivery.Result) }
	Config  struct {
		PoolSize, BatchSize                         int
		PollInterval, LeaseDuration, ReaperInterval time.Duration
		MaxAttempts                                 int
		Retry                                       backoff.Config
		CleanupTimeout                              time.Duration
	}
)

type Pool struct {
	q       queue.Queue
	s       delivery.Sender
	b       breaker.Breaker
	l       RateLimiter
	m       Metrics
	c       domain.Clock
	log     *slog.Logger
	cfg     Config
	running atomic.Int32
}

func New(q queue.Queue, s delivery.Sender, b breaker.Breaker, l RateLimiter, m Metrics, c domain.Clock, log *slog.Logger, cfg Config) *Pool {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Pool{q, s, b, l, m, c, log, cfg, atomic.Int32{}}
}
func (p *Pool) Running() int { return int(p.running.Load()) }
func (p *Pool) Run(c context.Context) error {
	var wg sync.WaitGroup
	wg.Add(p.cfg.PoolSize + 1)
	go func() { defer wg.Done(); p.reaper(c) }()
	for i := 0; i < p.cfg.PoolSize; i++ {
		n := i
		go func() { defer wg.Done(); p.loop(c, n) }()
	}
	<-c.Done()
	wg.Wait()
	return nil
}

func (p *Pool) loop(c context.Context, n int) {
	p.running.Add(1)
	defer p.running.Add(-1)
	id := fmt.Sprintf("worker-%d", n+1)
	log := p.log.With("worker_id", id)
	// #nosec G404 -- retry jitter does not require cryptographic randomness.
	rnd := rand.New(rand.NewSource(seed()))
	for {
		if c.Err() != nil {
			return
		}
		items, e := p.q.Claim(c, id, p.cfg.BatchSize, p.c.Now(), p.cfg.LeaseDuration)
		if e != nil {
			log.WarnContext(c, "queue_claim_failed", "error", e)
			p.wait(c, p.cfg.PollInterval)
			continue
		}
		if len(items) == 0 {
			p.wait(c, p.cfg.PollInterval)
			continue
		}
		for i, v := range items {
			if c.Err() != nil {
				for _, x := range items[i:] {
					p.release(x.Message.ID, p.c.Now(), p.c.Now())
				}
				return
			}
			p.process(c, rnd, v, log)
		}
	}
}

func (p *Pool) process(root context.Context, rnd *rand.Rand, v queue.ClaimedMessage, log *slog.Logger) {
	now := p.c.Now()
	ok, retry := p.b.Allow(root, v.Endpoint, now)
	if !ok {
		p.release(v.Message.ID, retry, now)
		return
	}
	if p.l != nil {
		ok, retry, e := p.l.Allow(root, v.Endpoint.ID, now)
		if e != nil {
			p.release(v.Message.ID, now.Add(p.cfg.PollInterval), now)
			return
		}
		if !ok {
			p.release(v.Message.ID, retry, now)
			return
		}
	}
	result := p.s.Send(context.WithoutCancel(root), v.Endpoint, v.Event, v.Message.ID, now)
	status := 0
	if result.StatusCode != nil {
		status = *result.StatusCode
	}
	log.InfoContext(root, "delivery_finished",
		"message_id", v.Message.ID,
		"endpoint_id", v.Endpoint.ID,
		"success", result.Success,
		"status", status,
		"duration", result.Duration,
	)
	if p.m != nil {
		p.m.ObserveDelivery(result)
	}
	done := p.c.Now()
	a := domain.Attempt{MessageID: v.Message.ID, AttemptNo: v.Message.Attempt + 1, RequestHeaders: result.SentHeaders, ResponseCode: result.StatusCode, ResponseSnippet: result.BodySnippet, Error: result.Error, Duration: result.Duration, CreatedAt: done}
	c, cancel := context.WithTimeout(context.Background(), p.cfg.CleanupTimeout)
	defer cancel()
	if result.Success {
		if p.q.Ack(c, v.Message.ID, a) == nil {
			_ = p.b.OnSuccess(c, v.Endpoint.ID, done)
		}
		return
	}
	next := done.Add(backoff.Next(v.Message.Attempt, p.cfg.Retry, rnd))
	if p.q.Nack(c, v.Message.ID, a, next, p.cfg.MaxAttempts) == nil {
		_ = p.b.OnFailure(c, v.Endpoint.ID, done)
	}
}

func (p *Pool) release(id domain.MessageID, next, now time.Time) {
	c, x := context.WithTimeout(context.Background(), p.cfg.CleanupTimeout)
	defer x()
	_ = p.q.Release(c, id, next, now)
}

func (p *Pool) reaper(c context.Context) {
	t := time.NewTicker(p.cfg.ReaperInterval)
	defer t.Stop()
	for {
		select {
		case <-c.Done():
			return
		case <-t.C:
			_, _ = p.q.Reap(c, p.c.Now())
		}
	}
}

func (p *Pool) wait(c context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.Done():
	case <-t.C:
	}
}

func seed() int64 {
	var b [8]byte
	if _, e := cryptorand.Read(b[:]); e != nil {
		return 1
	}
	return int64(binary.LittleEndian.Uint64(b[:]) >> 1)
}
