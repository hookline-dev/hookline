package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL                                                 string
	DBMaxConns                                                  int
	HTTPPort                                                    string
	HTTPReadTimeout, HTTPWriteTimeout, HTTPShutdownTimeout      time.Duration
	AppMode, LogLevel, LogFormat, AdminAPIKey                   string
	AutoMigrate                                                 bool
	WorkerPoolSize, WorkerBatchSize                             int
	WorkerPollInterval, QueueLeaseDuration, QueueReaperInterval time.Duration
	DeliveryTimeout                                             time.Duration
	DeliveryMaxResponseBytes                                    int64
	DeliveryBodySnippetBytes                                    int
	DeliveryUserAgent                                           string
	RetryBaseDelay, RetryMaxDelay                               time.Duration
	RetryMaxAttempts                                            int
	BreakerFailureThreshold                                     int
	BreakerOpenDuration, BreakerMaxOpenDuration                 time.Duration
	SignatureTolerance                                          time.Duration
	DefaultEndpointRateLimit                                    int
	IngestMaxBodyBytes                                          int64
}

func Load() (Config, error) {
	var c Config
	var es []error
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.HTTPPort = str("HTTP_PORT", "8080")
	c.AppMode = str("APP_MODE", "all")
	c.LogLevel = str("LOG_LEVEL", "info")
	c.LogFormat = str("LOG_FORMAT", "text")
	c.AdminAPIKey = str("ADMIN_API_KEY", "hk_dev_admin_change_me")
	c.DeliveryUserAgent = str("DELIVERY_USER_AGENT", "Hookline/1.0")
	c.DBMaxConns = num("DB_MAX_CONNS", 10, &es)
	c.WorkerPoolSize = num("WORKER_POOL_SIZE", 5, &es)
	c.WorkerBatchSize = num("WORKER_BATCH_SIZE", 10, &es)
	c.DeliveryMaxResponseBytes = int64(num("DELIVERY_MAX_RESPONSE_BYTES", 1<<20, &es))
	c.DeliveryBodySnippetBytes = num("DELIVERY_BODY_SNIPPET_BYTES", 1024, &es)
	c.RetryMaxAttempts = num("RETRY_MAX_ATTEMPTS", 8, &es)
	c.BreakerFailureThreshold = num("BREAKER_FAILURE_THRESHOLD", 10, &es)
	c.DefaultEndpointRateLimit = num("DEFAULT_ENDPOINT_RATE_LIMIT", 5, &es)
	c.IngestMaxBodyBytes = int64(num("INGEST_MAX_BODY_BYTES", 256<<10, &es))
	c.HTTPReadTimeout = dur("HTTP_READ_TIMEOUT", 10*time.Second, &es)
	c.HTTPWriteTimeout = dur("HTTP_WRITE_TIMEOUT", 15*time.Second, &es)
	c.HTTPShutdownTimeout = dur("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second, &es)
	c.WorkerPollInterval = dur("WORKER_POLL_INTERVAL", time.Second, &es)
	c.QueueLeaseDuration = dur("QUEUE_LEASE_DURATION", 30*time.Second, &es)
	c.QueueReaperInterval = dur("QUEUE_REAPER_INTERVAL", 15*time.Second, &es)
	c.DeliveryTimeout = dur("DELIVERY_TIMEOUT", 10*time.Second, &es)
	c.RetryBaseDelay = dur("RETRY_BASE_DELAY", 5*time.Second, &es)
	c.RetryMaxDelay = dur("RETRY_MAX_DELAY", 6*time.Hour, &es)
	c.BreakerOpenDuration = dur("BREAKER_OPEN_DURATION", 5*time.Minute, &es)
	c.BreakerMaxOpenDuration = dur("BREAKER_MAX_OPEN_DURATION", time.Hour, &es)
	c.SignatureTolerance = dur("SIGNATURE_TOLERANCE", 5*time.Minute, &es)
	c.AutoMigrate = boolean("AUTO_MIGRATE", false, &es)
	if c.DatabaseURL == "" {
		es = append(es, errors.New("DATABASE_URL is required"))
	}
	if c.QueueLeaseDuration <= c.DeliveryTimeout {
		es = append(es, errors.New("QUEUE_LEASE_DURATION must be greater than DELIVERY_TIMEOUT"))
	}
	if c.QueueReaperInterval >= c.QueueLeaseDuration {
		es = append(es, errors.New("QUEUE_REAPER_INTERVAL must be less than QUEUE_LEASE_DURATION"))
	}
	if c.RetryMaxAttempts < 1 {
		es = append(es, errors.New("RETRY_MAX_ATTEMPTS must be at least 1"))
	}
	if c.WorkerPoolSize < 1 || c.WorkerBatchSize < 1 {
		es = append(es, errors.New("worker sizes must be positive"))
	}
	if c.DBMaxConns < 1 || c.DBMaxConns > math.MaxInt32 {
		es = append(es, errors.New("DB_MAX_CONNS outside int32 range"))
	}
	if c.AppMode != "api" && c.AppMode != "worker" && c.AppMode != "all" {
		es = append(es, fmt.Errorf("invalid APP_MODE %q", c.AppMode))
	}
	return c, errors.Join(es...)
}

func str(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func num(k string, d int, es *[]error) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	n, e := strconv.Atoi(v)
	if e != nil {
		*es = append(*es, fmt.Errorf("%s: %w", k, e))
		return d
	}
	return n
}

func dur(k string, d time.Duration, es *[]error) time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	n, e := time.ParseDuration(v)
	if e != nil {
		*es = append(*es, fmt.Errorf("%s: %w", k, e))
		return d
	}
	return n
}

func boolean(k string, d bool, es *[]error) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	n, e := strconv.ParseBool(v)
	if e != nil {
		*es = append(*es, fmt.Errorf("%s: %w", k, e))
		return d
	}
	return n
}
