package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSinkStatusReceivedAndReset(t *testing.T) {
	s := &sink{}
	h := s.handler()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/hook?status=503", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", w.Code)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/received", nil))
	var items []received
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil || len(items) != 1 {
		t.Fatalf("received=%#v err=%v", items, err)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/reset", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("reset status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/received", nil))
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil || len(items) != 0 {
		t.Fatalf("received after reset=%#v err=%v", items, err)
	}
}

func TestSinkDelayValidation(t *testing.T) {
	h := (&sink{}).handler()
	for _, target := range []string{"/hook?status=bad", "/hook?delay=bad", "/hook?delay=2m"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d", target, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/hook?delay=1ms", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("valid delay status=%d", w.Code)
	}
}
