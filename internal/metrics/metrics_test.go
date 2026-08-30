package metrics

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookline/internal/delivery"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeStore struct {
	pending, inFlight, dead int64
	oldest                  float64
	err                     error
}

func (f *fakeStore) QueueStats(context.Context, time.Time) (int64, int64, int64, float64, error) {
	return f.pending, f.inFlight, f.dead, f.oldest, f.err
}

func TestRegistryCollectsQueueAndDeliveryMetrics(t *testing.T) {
	store := &fakeStore{pending: 4, inFlight: 2, dead: -1, oldest: 12.5}
	m := New(store, fakeClock{now: time.Unix(1_700_000_000, 0)})
	status := 204
	m.ObserveDelivery(delivery.Result{StatusCode: &status, Duration: 250 * time.Millisecond})
	m.ObserveDelivery(delivery.Result{Duration: time.Second})

	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`hookline_messages_pending 4`,
		`hookline_messages_in_flight 2`,
		`hookline_messages_dead_total 0`,
		`hookline_oldest_pending_seconds 12.5`,
		`hookline_delivery_total{code="204"} 1`,
		`hookline_delivery_total{code="network_error"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics do not contain %q", want)
		}
	}

	store.err = errors.New("database unavailable")
	w = httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("status after store error: %d", w.Code)
	}
}
