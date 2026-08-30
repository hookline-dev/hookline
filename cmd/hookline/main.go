package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/internal/api"
	"hookline/internal/backoff"
	"hookline/internal/breaker"
	"hookline/internal/config"
	"hookline/internal/delivery"
	"hookline/internal/domain"
	"hookline/internal/ingest"
	"hookline/internal/metrics"
	"hookline/internal/queue"
	"hookline/internal/ratelimit"
	"hookline/internal/storage/postgres"
	"hookline/internal/worker"
)

func main() {
	if e := run(); e != nil {
		slog.Error("hookline stopped", "error", e)
		os.Exit(1)
	}
}

func run() error {
	c, e := config.Load()
	if e != nil {
		return e
	}
	mode := flag.String("mode", c.AppMode, "api | worker | all")
	flag.Parse()
	if *mode != "api" && *mode != "worker" && *mode != "all" {
		return fmt.Errorf("invalid mode %q", *mode)
	}
	log := logger(c.LogLevel, c.LogFormat)
	slog.SetDefault(log)
	pc, e := pgxpool.ParseConfig(c.DatabaseURL)
	if e != nil {
		return e
	}
	// #nosec G115 -- config.Load validates that DBMaxConns fits in int32.
	pc.MaxConns = int32(c.DBMaxConns)
	db, e := pgxpool.NewWithConfig(context.Background(), pc)
	if e != nil {
		return e
	}
	defer db.Close()
	start, x := context.WithTimeout(context.Background(), 15*time.Second)
	defer x()
	if e = db.Ping(start); e != nil {
		return e
	}
	if c.AutoMigrate {
		if e = postgres.Migrate(start, db); e != nil {
			return e
		}
	}
	clock := domain.RealClock{}
	store := postgres.New(db)
	q := queue.New(db)
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConns: 200, MaxIdleConnsPerHost: 20, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	sender := delivery.New(&http.Client{Transport: transport}, clock, c.DeliveryTimeout, c.DeliveryMaxResponseBytes, c.DeliveryBodySnippetBytes, c.DeliveryUserAgent)
	br := breaker.New(store, c.BreakerFailureThreshold, c.BreakerOpenDuration, c.BreakerMaxOpenDuration)
	lim := ratelimit.New(store)
	met := metrics.New(store, clock)
	pool := worker.New(q, sender, br, lim, met, clock, log, worker.Config{PoolSize: c.WorkerPoolSize, BatchSize: c.WorkerBatchSize, PollInterval: c.WorkerPollInterval, LeaseDuration: c.QueueLeaseDuration, ReaperInterval: c.QueueReaperInterval, MaxAttempts: c.RetryMaxAttempts, Retry: backoff.Config{Base: c.RetryBaseDelay, Cap: c.RetryMaxDelay}, CleanupTimeout: c.HTTPShutdownTimeout})
	router := api.NewRouter(api.Dependencies{Repository: store, Ingest: ingest.New(store, c.IngestMaxBodyBytes), Clock: clock, Logger: log, AdminAPIKey: c.AdminAPIKey, MaxBodyBytes: c.IngestMaxBodyBytes, SignatureTolerance: c.SignatureTolerance, DefaultRateLimit: c.DefaultEndpointRateLimit, BreakerDefaultDuration: c.BreakerOpenDuration, RequireWorkersReady: *mode == "all", Workers: pool, MetricsHandler: met.Handler()})
	srv := &http.Server{Addr: ":" + c.HTTPPort, Handler: router, ReadTimeout: c.HTTPReadTimeout, ReadHeaderTimeout: c.HTTPReadTimeout, WriteTimeout: c.HTTPWriteTimeout, IdleTimeout: time.Minute}
	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(sig)
	defer cancel()
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	if *mode != "api" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if x := pool.Run(ctx); x != nil {
				errs <- x
			}
		}()
	}
	if *mode != "worker" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x := srv.ListenAndServe()
			if x != nil && !errors.Is(x, http.ErrServerClosed) {
				errs <- x
			}
		}()
	}
	select {
	case <-sig.Done():
	case e = <-errs:
	}
	cancel()
	down, done := context.WithTimeout(context.Background(), c.HTTPShutdownTimeout)
	defer done()
	if *mode != "worker" {
		_ = srv.Shutdown(down)
	}
	wg.Wait()
	return e
}

func logger(level, format string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	o := &slog.HandlerOptions{Level: l}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, o))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, o))
}
