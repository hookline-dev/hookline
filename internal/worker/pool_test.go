package worker

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"hookline/internal/backoff"
	"hookline/internal/delivery"
	"hookline/internal/domain"
	"hookline/internal/queue"
)

type fq struct {
	batch              []queue.ClaimedMessage
	ack, nack, release chan domain.MessageID
}

func (q *fq) Claim(context.Context, string, int, time.Time, time.Duration) ([]queue.ClaimedMessage, error) {
	b := q.batch
	q.batch = nil
	return b, nil
}

func (q *fq) Ack(_ context.Context, id domain.MessageID, _ domain.Attempt) error {
	q.ack <- id
	return nil
}

func (q *fq) Nack(_ context.Context, id domain.MessageID, _ domain.Attempt, _ time.Time, _ int) error {
	q.nack <- id
	return nil
}

func (q *fq) Release(_ context.Context, id domain.MessageID, _, _ time.Time) error {
	q.release <- id
	return nil
}
func (q *fq) Reap(context.Context, time.Time) (int, error) { return 0, nil }

type fs struct{ result delivery.Result }

func (s fs) Send(context.Context, domain.Endpoint, domain.Event, domain.MessageID, time.Time) delivery.Result {
	return s.result
}

type fb struct{ allow bool }

func (b fb) Allow(context.Context, domain.Endpoint, time.Time) (bool, time.Time) {
	return b.allow, time.Unix(1, 0).Add(time.Second)
}
func (fb) OnSuccess(context.Context, domain.EndpointID, time.Time) error { return nil }
func (fb) OnFailure(context.Context, domain.EndpointID, time.Time) error { return nil }

type fl struct{ allow bool }

func (l fl) Allow(context.Context, domain.EndpointID, time.Time) (bool, time.Time, error) {
	return l.allow, time.Unix(1, 0).Add(time.Second), nil
}

type fc struct{ now time.Time }

func (c fc) Now() time.Time { return c.now }
func TestPoolPaths(t *testing.T) {
	for _, c := range []struct {
		name                   string
		success, breaker, rate bool
		want                   string
	}{{"ack", true, true, true, "ack"}, {"nack", false, true, true, "nack"}, {"breaker", true, false, true, "release"}, {"rate", true, true, false, "release"}} {
		t.Run(c.name, func(t *testing.T) {
			q := &fq{batch: []queue.ClaimedMessage{{Message: domain.Message{ID: "m"}, Endpoint: domain.Endpoint{ID: "e"}}}, ack: make(chan domain.MessageID, 1), nack: make(chan domain.MessageID, 1), release: make(chan domain.MessageID, 1)}
			result := delivery.Result{Success: c.success, Error: "failed"}
			logger := slog.New(slog.DiscardHandler)
			if c.name == "ack" {
				status := 204
				result.StatusCode = &status
				logger = nil
			}
			p := New(q, fs{result}, fb{c.breaker}, fl{c.rate}, nil, fc{time.Unix(1, 0)}, logger, Config{PoolSize: 1, BatchSize: 1, PollInterval: time.Millisecond, LeaseDuration: time.Second, ReaperInterval: time.Hour, MaxAttempts: 2, Retry: backoff.Config{Base: time.Millisecond, Cap: time.Second}, CleanupTimeout: time.Second})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- p.Run(ctx) }()
			var ch <-chan domain.MessageID
			switch c.want {
			case "ack":
				ch = q.ack
			case "nack":
				ch = q.nack
			default:
				ch = q.release
			}
			select {
			case <-ch:
			case <-time.After(time.Second):
				t.Fatal("no result")
			}
			cancel()
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}
