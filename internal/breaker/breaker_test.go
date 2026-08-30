package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookline/internal/domain"
)

type fakeRepository struct {
	ep          domain.Endpoint
	err         error
	halfOpen    bool
	successes   int
	failures    int
	lastFailure time.Time
}

func (f *fakeRepository) GetEndpoint(context.Context, domain.EndpointID) (domain.Endpoint, error) {
	return f.ep, f.err
}

func (f *fakeRepository) TryHalfOpen(context.Context, domain.EndpointID, time.Time) (bool, error) {
	return f.halfOpen, f.err
}

func (f *fakeRepository) RecordBreakerSuccess(context.Context, domain.EndpointID, time.Duration) error {
	f.successes++
	return f.err
}

func (f *fakeRepository) RecordBreakerFailure(_ context.Context, _ domain.EndpointID, now time.Time, _ int, _ time.Duration) error {
	f.failures++
	f.lastFailure = now
	return f.err
}

func TestAllowStatesAndCallbacks(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := &fakeRepository{ep: domain.Endpoint{ID: "11111111-1111-4111-8111-111111111111", BreakerState: domain.BreakerClosed}}
	svc := New(repo, 3, time.Minute, time.Hour)

	if ok, _ := svc.Allow(context.Background(), repo.ep, now); !ok {
		t.Fatal("closed breaker denied")
	}
	repo.ep.Disabled = true
	if ok, retry := svc.Allow(context.Background(), repo.ep, now); ok || !retry.Equal(now.Add(time.Minute)) {
		t.Fatalf("disabled endpoint: ok=%v retry=%v", ok, retry)
	}
	repo.ep.Disabled = false
	repo.ep.BreakerState = domain.BreakerOpen
	repo.ep.BreakerOpenDuration = 10 * time.Second
	repo.ep.BreakerOpenedAt = nil
	if ok, _ := svc.Allow(context.Background(), repo.ep, now); ok {
		t.Fatal("open breaker without timestamp allowed")
	}
	opened := now.Add(-5 * time.Second)
	repo.ep.BreakerOpenedAt = &opened
	if ok, retry := svc.Allow(context.Background(), repo.ep, now); ok || !retry.Equal(opened.Add(10*time.Second)) {
		t.Fatalf("breaker should remain open: ok=%v retry=%v", ok, retry)
	}
	opened = now.Add(-20 * time.Second)
	repo.halfOpen = true
	if ok, _ := svc.Allow(context.Background(), repo.ep, now); !ok {
		t.Fatal("half-open probe denied")
	}
	repo.ep.BreakerState = domain.BreakerHalfOpen
	if ok, retry := svc.Allow(context.Background(), repo.ep, now); ok || !retry.Equal(now.Add(time.Second)) {
		t.Fatalf("unexpected half-open decision: ok=%v retry=%v", ok, retry)
	}

	if err := svc.OnSuccess(context.Background(), repo.ep.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.OnFailure(context.Background(), repo.ep.ID, now); err != nil {
		t.Fatal(err)
	}
	if repo.successes != 1 || repo.failures != 1 || !repo.lastFailure.Equal(now) {
		t.Fatalf("callbacks not recorded: %#v", repo)
	}
	repo.err = errors.New("database unavailable")
	if ok, _ := svc.Allow(context.Background(), repo.ep, now); ok {
		t.Fatal("repository error should deny delivery")
	}
}
