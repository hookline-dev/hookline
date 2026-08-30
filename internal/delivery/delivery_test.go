package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"hookline/internal/domain"
	"hookline/internal/signing"
)

type clock struct{ n time.Time }

func (c *clock) Now() time.Time { c.n = c.n.Add(time.Millisecond); return c.n }
func TestSend(t *testing.T) {
	now := time.Unix(1700000000, 0)
	secret := "secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		if signing.Verify(b, r.Header.Get("X-Hookline-Signature"), r.Header.Get("X-Hookline-Timestamp"), secret, now, time.Minute) != nil {
			t.Error("signature")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(strings.Repeat("ж", 100)))
	}))
	defer srv.Close()
	s := New(srv.Client(), &clock{}, time.Second, 1000, 7, "Hookline/1")
	v := s.Send(context.Background(), domain.Endpoint{URL: srv.URL, Secret: secret}, domain.Event{Type: "x", Payload: []byte(`{"x":1}`)}, "id", now)
	if !v.Success || v.StatusCode == nil || *v.StatusCode != 201 || !strings.Contains(v.SentHeaders["X-Hookline-Signature"], "REDACTED") || len(v.BodySnippet) > 7 || !utf8.ValidString(v.BodySnippet) {
		t.Fatalf("%#v", v)
	}
	bad := s.Send(context.Background(), domain.Endpoint{URL: "://"}, domain.Event{}, "id", now)
	if bad.Error == "" {
		t.Fatal("bad URL")
	}
}

func TestSendFailureModesAndResponseLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	t.Run("http failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		result := New(srv.Client(), &clock{}, time.Second, 1024, 1024, "test").Send(context.Background(), domain.Endpoint{URL: srv.URL}, domain.Event{Payload: []byte("{}")}, "id", now)
		if result.Success || result.StatusCode == nil || *result.StatusCode != http.StatusInternalServerError {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			<-release
		}))
		defer srv.Close()
		result := New(srv.Client(), &clock{}, 10*time.Millisecond, 1024, 1024, "test").Send(context.Background(), domain.Endpoint{URL: srv.URL}, domain.Event{Payload: []byte("{}")}, "id", now)
		close(release)
		if result.Success || result.StatusCode != nil || result.Error == "" {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("bounded body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", 10<<20)))
		}))
		defer srv.Close()
		result := New(srv.Client(), &clock{}, time.Second, 128, 32, "test").Send(context.Background(), domain.Endpoint{URL: srv.URL}, domain.Event{Payload: []byte("{}")}, "id", now)
		if !result.Success || len(result.BodySnippet) > 32 {
			t.Fatalf("unexpected result: %#v", result)
		}
	})
}
