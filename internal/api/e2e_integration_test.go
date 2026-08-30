//go:build integration

package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookline/internal/api"
	"hookline/internal/backoff"
	"hookline/internal/breaker"
	"hookline/internal/delivery"
	"hookline/internal/domain"
	"hookline/internal/ingest"
	"hookline/internal/queue"
	"hookline/internal/ratelimit"
	"hookline/internal/signing"
	storepostgres "hookline/internal/storage/postgres"
	"hookline/internal/testdb"
	"hookline/internal/worker"
)

func TestEndToEnd(t *testing.T) {
	db := testdb.Open(t)
	store := storepostgres.New(db)
	clock := domain.RealClock{}
	delivered := make(chan http.Header, 10)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { delivered <- r.Header.Clone(); w.WriteHeader(204) }))
	defer sink.Close()
	q := queue.New(db)
	circuit := breaker.New(store, 3, time.Second, time.Minute)
	limiter := ratelimit.New(store)
	pool := worker.New(q, delivery.New(sink.Client(), clock, time.Second, 1024, 1024, "Hookline/test"), circuit, limiter, nil, clock, slog.New(slog.DiscardHandler), worker.Config{PoolSize: 3, BatchSize: 10, PollInterval: time.Millisecond, LeaseDuration: time.Second, ReaperInterval: 100 * time.Millisecond, MaxAttempts: 3, Retry: backoff.Config{Base: time.Millisecond, Cap: time.Second}, CleanupTimeout: time.Second})
	router := api.NewRouter(api.Dependencies{Repository: store, Ingest: ingest.New(store, 4096), Clock: clock, Logger: slog.New(slog.DiscardHandler), AdminAPIKey: "admin", MaxBodyBytes: 4096, SignatureTolerance: time.Minute, DefaultRateLimit: 10, BreakerDefaultDuration: time.Second, Workers: pool})
	expectStatus(t, router, "", http.MethodGet, "/healthz", "", http.StatusOK)
	expectStatus(t, router, "", http.MethodGet, "/readyz", "", http.StatusOK)
	expectStatus(t, router, "", http.MethodGet, "/", "", http.StatusOK)
	expectStatus(t, router, "", http.MethodGet, "/api/v1/apps", "", http.StatusUnauthorized)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx) }()
	defer func() { cancel(); <-done }()
	app := request(t, router, "admin", http.MethodPost, "/api/v1/apps", `{"name":"e2e"}`)
	appID := app["id"].(string)
	appKey := app["apiKey"].(string)
	gh := app["githubWebhookSecret"].(string)
	expectStatus(t, router, appKey, http.MethodGet, "/api/v1/apps", "", http.StatusOK)
	ep := request(t, router, appKey, http.MethodPost, "/api/v1/apps/"+appID+"/endpoints", fmtJSON(map[string]any{"url": sink.URL, "secret": "endpoint-secret", "rateLimitRps": 10}))
	epID := ep["id"].(string)
	expectStatus(t, router, appKey, http.MethodGet, "/api/v1/apps/"+appID+"/endpoints", "", http.StatusOK)
	loaded, loadErr := store.GetEndpoint(context.Background(), domain.EndpointID(epID))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	allowed, retryAt := circuit.Allow(context.Background(), loaded, clock.Now())
	if !allowed {
		t.Fatalf("breaker denied %#v until %v", loaded, retryAt)
	}
	rateAllowed, rateRetry, rateErr := limiter.Allow(context.Background(), loaded.ID, clock.Now())
	if rateErr != nil || !rateAllowed {
		t.Fatalf("rate denied: allowed=%v retry=%v err=%v", rateAllowed, rateRetry, rateErr)
	}
	sub := request(t, router, appKey, http.MethodPost, "/api/v1/endpoints/"+epID+"/subscriptions", `{"eventType":"*"}`)
	subID := sub["id"].(string)
	verifyAt := clock.Now()
	verifyPayload := "verify me"
	request(t, router, appKey, http.MethodPost, "/api/v1/signatures/verify", fmtJSON(map[string]any{"payload": verifyPayload, "secret": "endpoint-secret", "timestamp": fmt.Sprint(verifyAt.Unix()), "signature": signing.Sign([]byte(verifyPayload), "endpoint-secret", verifyAt)}))
	first := request(t, router, "", http.MethodPost, "/ingest/"+appID, `{"order":42}`, map[string]string{"Content-Type": "application/json", "X-Event-Type": "order.created", "Idempotency-Key": "same"})
	if first["duplicate"].(bool) {
		t.Fatal("first duplicate")
	}
	second := request(t, router, "", http.MethodPost, "/ingest/"+appID, `{"order":42}`, map[string]string{"Content-Type": "application/json", "X-Event-Type": "order.created", "Idempotency-Key": "same"})
	if !second["duplicate"].(bool) {
		t.Fatal("not idempotent")
	}
	select {
	case h := <-delivered:
		if h.Get("X-Hookline-Signature") == "" {
			t.Fatal("unsigned")
		}
	case <-time.After(3 * time.Second):
		queued, listErr := store.ListMessages(context.Background(), domain.MessageFilter{Limit: 20})
		t.Fatalf("not delivered: messages=%#v err=%v", queued, listErr)
	}
	events := request(t, router, appKey, http.MethodGet, "/api/v1/events?limit=10", "")
	if len(events["items"].([]any)) == 0 {
		t.Fatal("events empty")
	}
	messages := request(t, router, appKey, http.MethodGet, "/api/v1/messages?limit=10", "")
	items := messages["items"].([]any)
	if len(items) == 0 {
		t.Fatal("messages empty")
	}
	mid := items[0].(map[string]any)["id"].(string)
	request(t, router, appKey, http.MethodGet, "/api/v1/messages/"+mid, "")
	request(t, router, appKey, http.MethodPost, "/api/v1/messages/"+mid+"/replay", "")
	body := []byte(`{"ref":"main"}`)
	mac := hmac.New(sha256.New, []byte(gh))
	_, _ = mac.Write(body)
	request(t, router, "", http.MethodPost, "/ingest/github/"+appID, string(body), map[string]string{"Content-Type": "application/json", "X-GitHub-Event": "push", "X-GitHub-Delivery": "gh-1", "X-Hub-Signature-256": "sha256=" + hex.EncodeToString(mac.Sum(nil))})
	request(t, router, appKey, http.MethodPost, "/api/v1/endpoints/"+epID+"/reset-breaker", "")
	request(t, router, appKey, http.MethodDelete, "/api/v1/subscriptions/"+subID, "")
	request(t, router, appKey, http.MethodDelete, "/api/v1/endpoints/"+epID, "")
}

func request(t *testing.T, h http.Handler, key, method, path, body string, extra ...map[string]string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if len(extra) > 0 {
		for k, v := range extra[0] {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	if w.Code == 204 {
		return map[string]any{}
	}
	var out map[string]any
	if e := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&out); e != nil {
		t.Fatal(e)
	}
	return out
}
func fmtJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func expectStatus(t *testing.T, h http.Handler, key, method, path, body string, want int) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
}
