package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type received struct {
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body"`
	ReceivedAt time.Time   `json:"receivedAt"`
}
type sink struct {
	mu    sync.RWMutex
	items []received
}

func (s *sink) handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /hook", s.hook)
	m.HandleFunc("GET /received", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		sinkWrite(w, 200, s.items)
	})
	m.HandleFunc("POST /reset", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.items = nil
		s.mu.Unlock()
		w.WriteHeader(204)
	})
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { sinkWrite(w, 200, map[string]string{"status": "ok"}) })
	return m
}

func (s *sink) hook(w http.ResponseWriter, r *http.Request) {
	b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		sinkWrite(w, 400, map[string]string{"error": "read"})
		return
	}
	s.mu.Lock()
	s.items = append(s.items, received{r.Header.Clone(), string(b), time.Now().UTC()})
	s.mu.Unlock()
	status := 200
	if v := r.URL.Query().Get("status"); v != "" {
		status, e = strconv.Atoi(v)
		if e != nil || status < 100 || status > 599 {
			sinkWrite(w, 400, map[string]string{"error": "status"})
			return
		}
	}
	if v := r.URL.Query().Get("delay"); v != "" {
		d, e := time.ParseDuration(v)
		if e != nil || d < 0 || d > time.Minute {
			sinkWrite(w, 400, map[string]string{"error": "delay"})
			return
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
		}
	}
	sinkWrite(w, status, map[string]any{"received": true, "bytes": len(b)})
}

func sinkWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	port := os.Getenv("SINK_PORT")
	if port == "" {
		port = "9090"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           (&sink{}).handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      70 * time.Second,
		IdleTimeout:       time.Minute,
	}
	_ = srv.ListenAndServe()
}
