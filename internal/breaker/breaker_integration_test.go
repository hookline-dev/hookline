//go:build integration

package breaker_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/internal/breaker"
	"hookline/internal/domain"
	storepostgres "hookline/internal/storage/postgres"
	"hookline/internal/testdb"
)

func TestPersistedBreakerTransitions(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t)
	store := storepostgres.New(db)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	appID := domain.AppID(newBreakerID(t))
	endpointID := domain.EndpointID(newBreakerID(t))
	const defaultDuration = 5 * time.Minute
	const maxDuration = time.Hour
	if err := store.CreateApp(ctx, domain.App{ID: appID, Name: "breaker", CreatedAt: now}, "breaker-key-hash", "github-webhook-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEndpoint(ctx, domain.Endpoint{
		ID:                  endpointID,
		AppID:               appID,
		URL:                 "https://example.test/hook",
		Secret:              "endpoint-secret",
		BreakerState:        domain.BreakerClosed,
		BreakerOpenDuration: defaultDuration,
		RateLimitRPS:        5,
		CreatedAt:           now,
	}); err != nil {
		t.Fatal(err)
	}
	service := breaker.New(store, 10, defaultDuration, maxDuration)

	for range 9 {
		if err := service.OnFailure(ctx, endpointID, now); err != nil {
			t.Fatal(err)
		}
	}
	ep := getBreakerEndpoint(t, store, endpointID)
	if ep.BreakerState != domain.BreakerClosed || ep.BreakerFailures != 9 {
		t.Fatalf("after 9 failures: state=%s failures=%d", ep.BreakerState, ep.BreakerFailures)
	}
	if err := service.OnFailure(ctx, endpointID, now); err != nil {
		t.Fatal(err)
	}
	ep = getBreakerEndpoint(t, store, endpointID)
	if ep.BreakerState != domain.BreakerOpen || ep.BreakerFailures != 10 || ep.BreakerOpenedAt == nil || !ep.BreakerOpenedAt.Equal(now) {
		t.Fatalf("after threshold: %#v", ep)
	}
	if allowed, retryAt := service.Allow(ctx, ep, now.Add(defaultDuration-time.Nanosecond)); allowed || !retryAt.Equal(now.Add(defaultDuration)) {
		t.Fatalf("open breaker allowed=%v retryAt=%v", allowed, retryAt)
	}
	if allowed, _ := service.Allow(ctx, ep, now.Add(defaultDuration)); !allowed {
		t.Fatal("expired breaker did not allow half-open probe")
	}
	if allowed, _ := service.Allow(ctx, ep, now.Add(defaultDuration)); allowed {
		t.Fatal("second half-open probe was allowed")
	}
	if err := service.OnSuccess(ctx, endpointID, now.Add(defaultDuration)); err != nil {
		t.Fatal(err)
	}
	ep = getBreakerEndpoint(t, store, endpointID)
	if ep.BreakerState != domain.BreakerClosed || ep.BreakerFailures != 0 || ep.BreakerOpenedAt != nil || ep.BreakerOpenDuration != defaultDuration {
		t.Fatalf("breaker was not reset: %#v", ep)
	}

	setHalfOpen(t, db, endpointID, defaultDuration)
	if err := service.OnFailure(ctx, endpointID, now); err != nil {
		t.Fatal(err)
	}
	ep = getBreakerEndpoint(t, store, endpointID)
	if ep.BreakerState != domain.BreakerOpen || ep.BreakerOpenDuration != 2*defaultDuration {
		t.Fatalf("half-open failure did not double duration: %#v", ep)
	}

	setHalfOpen(t, db, endpointID, 40*time.Minute)
	if err := service.OnFailure(ctx, endpointID, now); err != nil {
		t.Fatal(err)
	}
	ep = getBreakerEndpoint(t, store, endpointID)
	if ep.BreakerState != domain.BreakerOpen || ep.BreakerOpenDuration != maxDuration {
		t.Fatalf("open duration was not capped: %#v", ep)
	}
}

func setHalfOpen(t *testing.T, db *pgxpool.Pool, id domain.EndpointID, duration time.Duration) {
	t.Helper()
	_, err := db.Exec(context.Background(), `UPDATE endpoints SET breaker_state='half_open', breaker_probe_in_flight=true, breaker_open_duration=$2::bigint*interval '1 microsecond' WHERE id=$1::uuid`, id, duration.Microseconds())
	if err != nil {
		t.Fatal(err)
	}
}

func getBreakerEndpoint(t *testing.T, store *storepostgres.Store, id domain.EndpointID) domain.Endpoint {
	t.Helper()
	ep, err := store.GetEndpoint(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return ep
}

func newBreakerID(t *testing.T) string {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
