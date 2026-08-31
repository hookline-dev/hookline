package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"hookline/internal/domain"
	"hookline/internal/ingest"
	"hookline/internal/signing"
	webassets "hookline/web"
)

type repository interface {
	Ping(context.Context) error
	CreateApp(context.Context, domain.App, string, string) error
	GetApp(context.Context, domain.AppID) (domain.App, error)
	ListApps(context.Context) ([]domain.App, error)
	APIKeyAppID(context.Context, string) (domain.AppID, bool, error)
	GetGitHubSecret(context.Context, domain.AppID) (string, error)
	CreateEndpoint(context.Context, domain.Endpoint) error
	ListEndpoints(context.Context, domain.AppID) ([]domain.Endpoint, error)
	GetEndpoint(context.Context, domain.EndpointID) (domain.Endpoint, error)
	DisableEndpoint(context.Context, domain.EndpointID) error
	ResetBreaker(context.Context, domain.EndpointID, time.Duration) error
	CreateSubscription(context.Context, domain.Subscription) error
	GetSubscriptionAppID(context.Context, domain.SubscriptionID) (domain.AppID, error)
	DeleteSubscription(context.Context, domain.SubscriptionID) error
	ListEvents(context.Context, domain.EventFilter) ([]domain.Event, error)
	ListMessages(context.Context, domain.MessageFilter) ([]domain.Message, error)
	GetMessageDetail(context.Context, domain.MessageID) (domain.MessageDetail, error)
	ReplayMessage(context.Context, domain.MessageID, domain.MessageID, time.Time) (domain.Message, error)
}
type (
	WorkerHealth interface{ Running() int }
	Dependencies struct {
		Repository             repository
		Ingest                 ingest.DetailedService
		Clock                  domain.Clock
		Logger                 *slog.Logger
		AdminAPIKey            string
		MaxBodyBytes           int64
		SignatureTolerance     time.Duration
		DefaultRateLimit       int
		BreakerDefaultDuration time.Duration
		RequireWorkersReady    bool
		Workers                WorkerHealth
		MetricsHandler         http.Handler
	}
)

type server struct {
	Dependencies
	admin [32]byte
}

type requestIDKey struct{}
type authScopeKey struct{}

type authScope struct {
	admin bool
	appID domain.AppID
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func NewRouter(d Dependencies) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Clock == nil {
		d.Clock = domain.RealClock{}
	}
	s := &server{Dependencies: d, admin: sha256.Sum256([]byte(d.AdminAPIKey))}
	r := chi.NewRouter()
	r.Use(s.requestID, s.logger, s.recover)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", s.ready)
	if d.MetricsHandler != nil {
		r.Handle("/metrics", d.MetricsHandler)
	}
	r.Post("/ingest/{appID}", func(w http.ResponseWriter, r *http.Request) { s.accept(w, r, false) })
	r.Post("/ingest/github/{appID}", func(w http.ResponseWriter, r *http.Request) { s.accept(w, r, true) })
	r.Route("/api/v1", func(a chi.Router) {
		a.Use(s.auth)
		a.Post("/apps", s.createApp)
		a.Get("/apps", s.listApps)
		a.Post("/apps/{appID}/endpoints", s.createEndpoint)
		a.Get("/apps/{appID}/endpoints", s.listEndpoints)
		a.Delete("/endpoints/{endpointID}", s.disable)
		a.Post("/endpoints/{endpointID}/reset-breaker", s.reset)
		a.Post("/endpoints/{endpointID}/subscriptions", s.subscribe)
		a.Delete("/subscriptions/{subscriptionID}", s.unsubscribe)
		a.Get("/events", s.events)
		a.Get("/messages", s.messages)
		a.Get("/messages/{messageID}", s.detail)
		a.Post("/messages/{messageID}/replay", s.replay)
		a.Post("/signatures/verify", s.verify)
	})
	assets, e := fs.Sub(webassets.FS, "assets")
	if e == nil {
		r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	}
	for _, p := range []string{"/", "/settings", "/dlq", "/verify", "/messages/{messageID}"} {
		r.Get(p, func(w http.ResponseWriter, _ *http.Request) {
			b, _ := webassets.FS.ReadFile("index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
		})
	}
	return r
}

func (s *server) requestID(n http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 128 {
			var e error
			id, e = domain.NewID()
			if e != nil {
				mapError(w, e)
				return
			}
		}
		w.Header().Set("X-Request-Id", id)
		n.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func (s *server) logger(n http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := s.Clock.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		n.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		s.Logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
			slog.String("request_id", requestID(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Duration("duration", s.Clock.Now().Sub(started)),
		)
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func (s *server) recover(n http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				s.Logger.ErrorContext(r.Context(), "http_panic",
					"request_id", requestID(r.Context()),
					"panic", value,
					"stack", string(debug.Stack()),
				)
				problem(w, 500, "internal", "internal server error")
			}
		}()
		n.ServeHTTP(w, r)
	})
}

func (s *server) auth(n http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		h := sha256.Sum256([]byte(v))
		scope := authScope{admin: v != "" && subtle.ConstantTimeCompare(h[:], s.admin[:]) == 1}
		if !scope.admin {
			var ok bool
			var e error
			scope.appID, ok, e = s.Repository.APIKeyAppID(r.Context(), hex.EncodeToString(h[:]))
			if e != nil {
				mapError(w, e)
				return
			}
			if !ok {
				mapError(w, domain.ErrUnauthorized)
				return
			}
		}
		if v == "" {
			mapError(w, domain.ErrUnauthorized)
			return
		}
		n.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authScopeKey{}, scope)))
	})
}

func scope(ctx context.Context) authScope {
	v, _ := ctx.Value(authScopeKey{}).(authScope)
	return v
}

func (s *server) authorizeApp(ctx context.Context, id domain.AppID) error {
	v := scope(ctx)
	if v.admin || v.appID == id {
		return nil
	}
	return domain.ErrNotFound
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	parts := map[string]string{"database": "ok"}
	status := 200
	if s.Repository.Ping(r.Context()) != nil {
		status = 503
		parts["database"] = "unavailable"
	}
	if s.RequireWorkersReady && (s.Workers == nil || s.Workers.Running() == 0) {
		status = 503
		parts["workers"] = "unavailable"
	}
	write(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "unavailable"}[status == 200], "components": parts})
}

func decode(w http.ResponseWriter, r *http.Request, max int64, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		var m *http.MaxBytesError
		if errors.As(e, &m) {
			return domain.ErrPayloadTooLarge
		}
		return fmt.Errorf("%w: %w", domain.ErrInvalidInput, e)
	}
	if d.Decode(&struct{}{}) == nil {
		return domain.ErrInvalidInput
	}
	return nil
}

func path(r *http.Request, k string) (string, error) {
	v := chi.URLParam(r, k)
	if !domain.ValidID(v) {
		return "", domain.ErrInvalidInput
	}
	return v, nil
}

func token(prefix string) (string, error) {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *server) createApp(w http.ResponseWriter, r *http.Request) {
	if !scope(r.Context()).admin {
		mapError(w, domain.ErrUnauthorized)
		return
	}
	var q struct {
		Name   string `json:"name"`
		GitHub string `json:"githubWebhookSecret,omitempty"`
	}
	if e := decode(w, r, s.MaxBodyBytes, &q); e != nil {
		mapError(w, e)
		return
	}
	q.Name = strings.TrimSpace(q.Name)
	q.GitHub = strings.TrimSpace(q.GitHub)
	if q.Name == "" || len(q.Name) > 200 {
		mapError(w, domain.ErrInvalidInput)
		return
	}
	id, e := domain.NewID()
	if e != nil {
		mapError(w, e)
		return
	}
	key, e := token("hk_live_")
	if e != nil {
		mapError(w, e)
		return
	}
	if len(q.GitHub) < 16 || len(q.GitHub) > 512 {
		mapError(w, domain.ErrInvalidInput)
		return
	}
	a := domain.App{ID: domain.AppID(id), Name: q.Name, CreatedAt: s.Clock.Now()}
	h := sha256.Sum256([]byte(key))
	if e = s.Repository.CreateApp(r.Context(), a, hex.EncodeToString(h[:]), q.GitHub); e != nil {
		mapError(w, e)
		return
	}
	write(w, 201, map[string]any{"id": a.ID, "name": a.Name, "apiKey": key})
}

func (s *server) listApps(w http.ResponseWriter, r *http.Request) {
	if v := scope(r.Context()); !v.admin {
		a, e := s.Repository.GetApp(r.Context(), v.appID)
		if e != nil {
			mapError(w, e)
			return
		}
		write(w, 200, []domain.App{a})
		return
	}
	v, e := s.Repository.ListApps(r.Context())
	if e != nil {
		mapError(w, e)
		return
	}
	write(w, 200, v)
}

func (s *server) createEndpoint(w http.ResponseWriter, r *http.Request) {
	a, e := path(r, "appID")
	if e != nil {
		mapError(w, e)
		return
	}
	if e = s.authorizeApp(r.Context(), domain.AppID(a)); e != nil {
		mapError(w, e)
		return
	}
	var q struct {
		URL, Secret string
		Rate        int `json:"rateLimitRps"`
	}
	if e = decode(w, r, s.MaxBodyBytes, &q); e != nil {
		mapError(w, e)
		return
	}
	u, x := url.ParseRequestURI(q.URL)
	if x != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || q.Secret == "" || q.Rate < 0 {
		mapError(w, domain.ErrInvalidInput)
		return
	}
	if q.Rate == 0 {
		q.Rate = s.DefaultRateLimit
	}
	id, e := domain.NewID()
	if e != nil {
		mapError(w, e)
		return
	}
	v := domain.Endpoint{ID: domain.EndpointID(id), AppID: domain.AppID(a), URL: q.URL, Secret: q.Secret, BreakerState: domain.BreakerClosed, BreakerOpenDuration: s.BreakerDefaultDuration, RateLimitRPS: q.Rate, CreatedAt: s.Clock.Now()}
	if e = s.Repository.CreateEndpoint(r.Context(), v); e != nil {
		mapError(w, e)
		return
	}
	write(w, 201, endpointDTO(v))
}

func endpointDTO(v domain.Endpoint) map[string]any {
	st := "active"
	if v.Disabled {
		st = "disabled"
	}
	return map[string]any{"id": v.ID, "appId": v.AppID, "url": v.URL, "status": st, "breakerState": v.BreakerState, "breakerFailures": v.BreakerFailures, "rateLimitRps": v.RateLimitRPS, "createdAt": v.CreatedAt}
}

func (s *server) listEndpoints(w http.ResponseWriter, r *http.Request) {
	a, e := path(r, "appID")
	if e != nil {
		mapError(w, e)
		return
	}
	if e = s.authorizeApp(r.Context(), domain.AppID(a)); e != nil {
		mapError(w, e)
		return
	}
	v, e := s.Repository.ListEndpoints(r.Context(), domain.AppID(a))
	if e != nil {
		mapError(w, e)
		return
	}
	out := make([]any, 0, len(v))
	for _, x := range v {
		out = append(out, endpointDTO(x))
	}
	write(w, 200, out)
}

func (s *server) disable(w http.ResponseWriter, r *http.Request) {
	id, e := path(r, "endpointID")
	if e == nil {
		var ep domain.Endpoint
		ep, e = s.Repository.GetEndpoint(r.Context(), domain.EndpointID(id))
		if e == nil {
			e = s.authorizeApp(r.Context(), ep.AppID)
		}
	}
	if e == nil {
		e = s.Repository.DisableEndpoint(r.Context(), domain.EndpointID(id))
	}
	if e != nil {
		mapError(w, e)
		return
	}
	w.WriteHeader(204)
}

func (s *server) reset(w http.ResponseWriter, r *http.Request) {
	id, e := path(r, "endpointID")
	if e == nil {
		var ep domain.Endpoint
		ep, e = s.Repository.GetEndpoint(r.Context(), domain.EndpointID(id))
		if e == nil {
			e = s.authorizeApp(r.Context(), ep.AppID)
		}
	}
	if e == nil {
		e = s.Repository.ResetBreaker(r.Context(), domain.EndpointID(id), s.BreakerDefaultDuration)
	}
	if e != nil {
		mapError(w, e)
		return
	}
	w.WriteHeader(204)
}

func (s *server) subscribe(w http.ResponseWriter, r *http.Request) {
	id, e := path(r, "endpointID")
	var q struct {
		EventType string `json:"eventType"`
	}
	if e == nil {
		e = decode(w, r, s.MaxBodyBytes, &q)
	}
	if e == nil {
		var ep domain.Endpoint
		ep, e = s.Repository.GetEndpoint(r.Context(), domain.EndpointID(id))
		if e == nil {
			e = s.authorizeApp(r.Context(), ep.AppID)
		}
	}
	if e == nil && !pattern(q.EventType) {
		e = domain.ErrInvalidEventType
	}
	if e != nil {
		mapError(w, e)
		return
	}
	x, e := domain.NewID()
	if e != nil {
		mapError(w, e)
		return
	}
	v := domain.Subscription{ID: domain.SubscriptionID(x), EndpointID: domain.EndpointID(id), EventType: q.EventType, CreatedAt: s.Clock.Now()}
	if e = s.Repository.CreateSubscription(r.Context(), v); e != nil {
		mapError(w, e)
		return
	}
	write(w, 201, v)
}

func (s *server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	id, e := path(r, "subscriptionID")
	if e == nil {
		var appID domain.AppID
		appID, e = s.Repository.GetSubscriptionAppID(r.Context(), domain.SubscriptionID(id))
		if e == nil {
			e = s.authorizeApp(r.Context(), appID)
		}
	}
	if e == nil {
		e = s.Repository.DeleteSubscription(r.Context(), domain.SubscriptionID(id))
	}
	if e != nil {
		mapError(w, e)
		return
	}
	w.WriteHeader(204)
}

func (s *server) accept(w http.ResponseWriter, r *http.Request, github bool) {
	id, e := path(r, "appID")
	if e != nil {
		mapError(w, e)
		return
	}
	m, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || m != "application/json" {
		problem(w, 415, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.MaxBodyBytes)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		mapError(w, domain.ErrPayloadTooLarge)
		return
	}
	typ := r.Header.Get("X-Event-Type")
	idem := r.Header.Get("Idempotency-Key")
	if github {
		secret, x := s.Repository.GetGitHubSecret(r.Context(), domain.AppID(id))
		if x != nil {
			mapError(w, x)
			return
		}
		if x = signing.VerifyGitHub(b, r.Header.Get("X-Hub-Signature-256"), secret); x != nil {
			mapError(w, x)
			return
		}
		ev := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
		if ev == "" {
			mapError(w, domain.ErrInvalidEventType)
			return
		}
		typ = "github." + ev
		if idem == "" {
			idem = r.Header.Get("X-GitHub-Delivery")
		}
	}
	v, e := s.Ingest.AcceptDetailed(r.Context(), ingest.Request{AppID: domain.AppID(id), EventType: typ, Payload: b, IdemKey: idem}, s.Clock.Now())
	if e != nil {
		mapError(w, e)
		return
	}
	write(w, 202, map[string]any{"eventId": v.Event.ID, "messagesCreated": v.MessagesCreated, "duplicate": v.Duplicate})
}

func limit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 50, nil
	}
	v, e := strconv.Atoi(raw)
	if e != nil || v < 1 || v > 200 {
		return 0, domain.ErrInvalidInput
	}
	return v, nil
}

func cursor(raw string) (*domain.Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	var c domain.Cursor
	if e != nil || json.Unmarshal(b, &c) != nil || c.CreatedAt.IsZero() || !domain.ValidID(c.ID) {
		return nil, domain.ErrInvalidInput
	}
	return &c, nil
}

func next(c domain.Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	n, e := limit(r)
	if e != nil {
		mapError(w, e)
		return
	}
	c, e := cursor(r.URL.Query().Get("cursor"))
	if e != nil {
		mapError(w, e)
		return
	}
	v, e := s.Repository.ListEvents(r.Context(), domain.EventFilter{AppID: scope(r.Context()).appID, Type: r.URL.Query().Get("type"), Before: c, Limit: n + 1})
	if e != nil {
		mapError(w, e)
		return
	}
	cur := ""
	if len(v) > n {
		x := v[n-1]
		cur = next(domain.Cursor{CreatedAt: x.ReceivedAt, ID: string(x.ID)})
		v = v[:n]
	}
	write(w, 200, map[string]any{"items": v, "nextCursor": cur})
}

func (s *server) messages(w http.ResponseWriter, r *http.Request) {
	n, e := limit(r)
	if e != nil {
		mapError(w, e)
		return
	}
	c, e := cursor(r.URL.Query().Get("cursor"))
	if e != nil {
		mapError(w, e)
		return
	}
	st := domain.MessageStatus(r.URL.Query().Get("status"))
	if st != "" && st != domain.StatusPending && st != domain.StatusInFlight && st != domain.StatusDelivered && st != domain.StatusDead {
		mapError(w, domain.ErrInvalidInput)
		return
	}
	ep := domain.EndpointID(r.URL.Query().Get("endpointId"))
	if ep != "" && !domain.ValidID(string(ep)) {
		mapError(w, domain.ErrInvalidInput)
		return
	}
	v, e := s.Repository.ListMessages(r.Context(), domain.MessageFilter{AppID: scope(r.Context()).appID, Status: st, EndpointID: ep, Before: c, Limit: n + 1})
	if e != nil {
		mapError(w, e)
		return
	}
	cur := ""
	if len(v) > n {
		x := v[n-1]
		cur = next(domain.Cursor{CreatedAt: x.CreatedAt, ID: string(x.ID)})
		v = v[:n]
	}
	write(w, 200, map[string]any{"items": v, "nextCursor": cur})
}

func (s *server) detail(w http.ResponseWriter, r *http.Request) {
	id, e := path(r, "messageID")
	if e != nil {
		mapError(w, e)
		return
	}
	v, e := s.Repository.GetMessageDetail(r.Context(), domain.MessageID(id))
	if e != nil {
		mapError(w, e)
		return
	}
	if e = s.authorizeApp(r.Context(), v.Event.AppID); e != nil {
		mapError(w, e)
		return
	}
	write(w, 200, map[string]any{"id": v.Message.ID, "status": v.Message.Status, "attempt": v.Message.Attempt, "nextAttemptAt": v.Message.NextAttemptAt, "replayOf": v.Message.ReplayOf, "event": v.Event, "endpoint": endpointDTO(v.Endpoint), "attempts": v.Attempts})
}

func (s *server) replay(w http.ResponseWriter, r *http.Request) {
	id, e := path(r, "messageID")
	if e != nil {
		mapError(w, e)
		return
	}
	detail, e := s.Repository.GetMessageDetail(r.Context(), domain.MessageID(id))
	if e == nil {
		e = s.authorizeApp(r.Context(), detail.Event.AppID)
	}
	if e != nil {
		mapError(w, e)
		return
	}
	n, e := domain.NewID()
	if e != nil {
		mapError(w, e)
		return
	}
	v, e := s.Repository.ReplayMessage(r.Context(), domain.MessageID(id), domain.MessageID(n), s.Clock.Now())
	if e != nil {
		mapError(w, e)
		return
	}
	write(w, 202, map[string]any{"newMessageId": v.ID, "replayOf": v.ReplayOf})
}

func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	var q struct{ Payload, Secret, Timestamp, Signature string }
	if e := decode(w, r, s.MaxBodyBytes, &q); e != nil {
		mapError(w, e)
		return
	}
	if e := signing.Verify([]byte(q.Payload), q.Signature, q.Timestamp, q.Secret, s.Clock.Now(), s.SignatureTolerance); e != nil {
		mapError(w, e)
		return
	}
	write(w, 200, map[string]bool{"valid": true})
}

func pattern(v string) bool {
	return v != "" && !strings.ContainsAny(v, " \t\r\n") && (v == "*" || !strings.Contains(v, "*") || (strings.HasSuffix(v, ".*") && strings.Count(v, "*") == 1))
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func mapError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, domain.ErrNotFound):
		problem(w, 404, "not_found", "resource not found")
	case errors.Is(e, domain.ErrInvalidSignature):
		problem(w, 401, "invalid_signature", "signature verification failed")
	case errors.Is(e, domain.ErrSignatureExpired):
		problem(w, 401, "signature_expired", "signature timestamp is outside the tolerance")
	case errors.Is(e, domain.ErrUnauthorized):
		problem(w, 401, "unauthorized", "valid credentials required")
	case errors.Is(e, domain.ErrEndpointDisabled):
		problem(w, 409, "endpoint_disabled", "endpoint is disabled")
	case errors.Is(e, domain.ErrConflict):
		problem(w, 409, "conflict", "resource conflict")
	case errors.Is(e, domain.ErrPayloadTooLarge):
		problem(w, 413, "payload_too_large", "request body is too large")
	case errors.Is(e, domain.ErrInvalidEventType):
		problem(w, 400, "invalid_event_type", "event type or subscription pattern is invalid")
	case errors.Is(e, domain.ErrInvalidInput), errors.Is(e, domain.ErrInvalidJSON):
		problem(w, 400, "invalid_input", "request contains invalid fields")
	default:
		problem(w, 500, "internal", "internal server error")
	}
}
