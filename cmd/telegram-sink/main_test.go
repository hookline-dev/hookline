package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hookline/internal/signing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func signedRequest(t *testing.T, secret string) *http.Request {
	t.Helper()
	now := time.Now()
	body := []byte(`{"ref":"main"}`)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	r.Header.Set("X-Hookline-Timestamp", strconv.FormatInt(now.Unix(), 10))
	r.Header.Set("X-Hookline-Signature", signing.Sign(body, secret, now))
	return r
}

func TestHandlerForwardsValidDelivery(t *testing.T) {
	called := false
	s := &service{token: "token", chat: "42", secret: "endpoint-secret", tolerance: time.Minute, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, signedRequest(t, s.secret))
	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v body=%s", w.Code, called, w.Body.String())
	}
}

func TestHandlerRejectsInvalidSignature(t *testing.T) {
	s := &service{secret: "endpoint-secret", tolerance: time.Minute, client: http.DefaultClient}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	s.handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestHandlerDoesNotLeakToken(t *testing.T) {
	const token = "super-secret-token"
	s := &service{token: token, chat: "42", secret: "endpoint-secret", tolerance: time.Minute, client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request to https://api.telegram.org/bot" + token + "/sendMessage failed")
	})}}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, signedRequest(t, s.secret))
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), token) {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}
