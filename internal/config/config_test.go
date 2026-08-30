package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("HTTP_PORT", " 9090 ")
	t.Setenv("APP_MODE", "worker")
	t.Setenv("AUTO_MIGRATE", "true")
	t.Setenv("DB_MAX_CONNS", "17")
	t.Setenv("DELIVERY_TIMEOUT", "2s")
	t.Setenv("QUEUE_LEASE_DURATION", "9s")
	t.Setenv("QUEUE_REAPER_INTERVAL", "3s")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPPort != "9090" || c.AppMode != "worker" || !c.AutoMigrate || c.DBMaxConns != 17 {
		t.Fatalf("unexpected config: %#v", c)
	}
	if c.DeliveryTimeout != 2*time.Second || c.QueueLeaseDuration != 9*time.Second {
		t.Fatalf("unexpected durations: %#v", c)
	}
}

func TestLoadReportsAllInvalidValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("APP_MODE", "invalid")
	t.Setenv("DB_MAX_CONNS", "0")
	t.Setenv("WORKER_POOL_SIZE", "0")
	t.Setenv("WORKER_BATCH_SIZE", "0")
	t.Setenv("RETRY_MAX_ATTEMPTS", "0")
	t.Setenv("DELIVERY_TIMEOUT", "bad")
	t.Setenv("QUEUE_LEASE_DURATION", "1s")
	t.Setenv("QUEUE_REAPER_INTERVAL", "2s")
	t.Setenv("AUTO_MIGRATE", "perhaps")

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"DATABASE_URL", "APP_MODE", "DB_MAX_CONNS", "worker sizes", "RETRY_MAX_ATTEMPTS", "DELIVERY_TIMEOUT", "QUEUE_REAPER_INTERVAL", "AUTO_MIGRATE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
