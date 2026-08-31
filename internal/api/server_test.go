package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookline/internal/domain"
)

type repositoryStub struct {
	keyHash string
	app     domain.App
	detail  domain.MessageDetail
	created bool
	secret  string
}

func (r *repositoryStub) Ping(context.Context) error { return nil }
func (r *repositoryStub) CreateApp(_ context.Context, app domain.App, _, secret string) error {
	r.app, r.created, r.secret = app, true, secret
	return nil
}
func (r *repositoryStub) GetApp(context.Context, domain.AppID) (domain.App, error) {
	return r.app, nil
}
func (r *repositoryStub) ListApps(context.Context) ([]domain.App, error) {
	return []domain.App{r.app}, nil
}
func (r *repositoryStub) APIKeyAppID(_ context.Context, hash string) (domain.AppID, bool, error) {
	return r.app.ID, hash == r.keyHash, nil
}
func (r *repositoryStub) GetGitHubSecret(context.Context, domain.AppID) (string, error) {
	return r.secret, nil
}
func (r *repositoryStub) CreateEndpoint(context.Context, domain.Endpoint) error { return nil }
func (r *repositoryStub) ListEndpoints(context.Context, domain.AppID) ([]domain.Endpoint, error) {
	return nil, nil
}
func (r *repositoryStub) GetEndpoint(context.Context, domain.EndpointID) (domain.Endpoint, error) {
	return domain.Endpoint{AppID: r.app.ID}, nil
}
func (r *repositoryStub) DisableEndpoint(context.Context, domain.EndpointID) error { return nil }
func (r *repositoryStub) ResetBreaker(context.Context, domain.EndpointID, time.Duration) error {
	return nil
}
func (r *repositoryStub) CreateSubscription(context.Context, domain.Subscription) error { return nil }
func (r *repositoryStub) GetSubscriptionAppID(context.Context, domain.SubscriptionID) (domain.AppID, error) {
	return r.app.ID, nil
}
func (r *repositoryStub) DeleteSubscription(context.Context, domain.SubscriptionID) error {
	return nil
}
func (r *repositoryStub) ListEvents(context.Context, domain.EventFilter) ([]domain.Event, error) {
	return nil, nil
}
func (r *repositoryStub) ListMessages(context.Context, domain.MessageFilter) ([]domain.Message, error) {
	return nil, nil
}
func (r *repositoryStub) GetMessageDetail(context.Context, domain.MessageID) (domain.MessageDetail, error) {
	if r.detail.Event.AppID != "" {
		return r.detail, nil
	}
	return domain.MessageDetail{Event: domain.Event{AppID: r.app.ID}}, nil
}
func (r *repositoryStub) ReplayMessage(context.Context, domain.MessageID, domain.MessageID, time.Time) (domain.Message, error) {
	return domain.Message{}, nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func testRouter(repo *repositoryStub) http.Handler {
	return NewRouter(Dependencies{
		Repository:             repo,
		Clock:                  fixedClock{time.Unix(1700000000, 0)},
		Logger:                 slog.New(slog.DiscardHandler),
		AdminAPIKey:            "admin",
		MaxBodyBytes:           4096,
		DefaultRateLimit:       5,
		BreakerDefaultDuration: time.Minute,
	})
}

func apiRequest(h http.Handler, key, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthorizationAndInputValidation(t *testing.T) {
	const appID = "018f47a6-5f64-4e42-8a13-f7d2a2f77777"
	hash := sha256.Sum256([]byte("app-key"))
	repo := &repositoryStub{keyHash: hex.EncodeToString(hash[:]), app: domain.App{ID: appID, Name: "one"}}
	h := testRouter(repo)
	cases := []struct {
		name, key, method, target, body string
		status                          int
	}{
		{"missing key", "", http.MethodGet, "/api/v1/apps", "", http.StatusUnauthorized},
		{"app cannot create apps", "app-key", http.MethodPost, "/api/v1/apps", `{"name":"x","githubWebhookSecret":"1234567890123456"}`, http.StatusUnauthorized},
		{"cross app hidden", "app-key", http.MethodGet, "/api/v1/apps/018f47a6-5f64-4e42-8a13-f7d2a2f78888/endpoints", "", http.StatusNotFound},
		{"negative rate", "app-key", http.MethodPost, "/api/v1/apps/" + appID + "/endpoints", `{"url":"https://example.com/hook","secret":"secret","rateLimitRps":-1}`, http.StatusBadRequest},
		{"short github secret", "admin", http.MethodPost, "/api/v1/apps", `{"name":"x","githubWebhookSecret":"short"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := apiRequest(h, tc.key, tc.method, tc.target, tc.body); got.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", got.Code, tc.status, got.Body.String())
			}
		})
	}
}

func TestCreateAppDoesNotExposeGitHubSecret(t *testing.T) {
	repo := &repositoryStub{}
	w := apiRequest(testRouter(repo), "admin", http.MethodPost, "/api/v1/apps", `{"name":"safe","githubWebhookSecret":"github-secret-1234"}`)
	if w.Code != http.StatusCreated || !repo.created || repo.secret != "github-secret-1234" {
		t.Fatalf("status=%d created=%v secret=%q body=%s", w.Code, repo.created, repo.secret, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["githubWebhookSecret"]; exists {
		t.Fatal("GitHub secret was exposed")
	}
	if body["apiKey"] == "" {
		t.Fatal("one-time API key missing")
	}
}

func TestErrorMapping(t *testing.T) {
	for err, want := range map[error]int{
		domain.ErrNotFound:        http.StatusNotFound,
		domain.ErrUnauthorized:    http.StatusUnauthorized,
		domain.ErrConflict:        http.StatusConflict,
		domain.ErrPayloadTooLarge: http.StatusRequestEntityTooLarge,
		domain.ErrInvalidInput:    http.StatusBadRequest,
	} {
		w := httptest.NewRecorder()
		mapError(w, err)
		if w.Code != want {
			t.Fatalf("%v: got %d, want %d", err, w.Code, want)
		}
	}
}

func TestMessageDetailAttemptDTO(t *testing.T) {
	const appID = "018f47a6-5f64-4e42-8a13-f7d2a2f77777"
	const messageID = "018f47a6-5f64-4e42-8a13-f7d2a2f76666"
	code := http.StatusBadGateway
	repo := &repositoryStub{
		app: domain.App{ID: appID, Name: "one"},
		detail: domain.MessageDetail{
			Message: domain.Message{ID: messageID, Status: domain.StatusDead, NextAttemptAt: time.Unix(1_700_000_100, 0)},
			Event:   domain.Event{AppID: appID},
			Attempts: []domain.Attempt{{
				AttemptNo:       2,
				RequestHeaders:  map[string]string{"Authorization": "secret"},
				ResponseCode:    &code,
				ResponseSnippet: "upstream failed",
				Duration:        1250 * time.Millisecond,
				CreatedAt:       time.Unix(1_700_000_001, 0),
			}},
		},
	}
	w := apiRequest(testRouter(repo), "admin", http.MethodGet, "/api/v1/messages/"+messageID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Attempts      []map[string]any `json:"attempts"`
		NextAttemptAt any              `json:"nextAttemptAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Attempts) != 1 || body.Attempts[0]["durationMs"] != float64(1250) {
		t.Fatalf("attempts=%#v", body.Attempts)
	}
	if _, exposed := body.Attempts[0]["requestHeaders"]; exposed {
		t.Fatal("request headers were exposed")
	}
	if body.NextAttemptAt != nil {
		t.Fatalf("dead message nextAttemptAt=%v, want null", body.NextAttemptAt)
	}
}
