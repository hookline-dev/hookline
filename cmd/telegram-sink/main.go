package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"hookline/internal/signing"
)

type service struct {
	token, chat, secret string
	tolerance           time.Duration
	client              *http.Client
}

func (s *service) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if e != nil || signing.Verify(b, r.Header.Get("X-Hookline-Signature"), r.Header.Get("X-Hookline-Timestamp"), s.secret, time.Now(), s.tolerance) != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		text := "🪝 Hookline " + r.Header.Get("X-Hookline-Event-Type") + "\n" + string(b)
		payload, _ := json.Marshal(map[string]string{"chat_id": s.chat, "text": text})
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://api.telegram.org/bot"+s.token+"/sendMessage", bytes.NewReader(payload))
		if req == nil {
			http.Error(w, "telegram request failed", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := s.client.Do(req)
		if e != nil {
			// Never echo the upstream error: it can contain the bot token from the URL.
			http.Error(w, "telegram request failed", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode/100 != 2 {
			http.Error(w, "telegram rejected", http.StatusBadGateway)
			return
		}
		w.WriteHeader(204)
	})
}

func main() {
	d, _ := time.ParseDuration(os.Getenv("SIGNATURE_TOLERANCE"))
	if d == 0 {
		d = 5 * time.Minute
	}
	s := &service{os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID"), os.Getenv("HOOKLINE_ENDPOINT_SECRET"), d, &http.Client{Timeout: 10 * time.Second}}
	srv := &http.Server{
		Addr:              ":9092",
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
	}
	_ = srv.ListenAndServe()
}
