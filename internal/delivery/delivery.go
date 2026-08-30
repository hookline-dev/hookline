package delivery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"hookline/internal/attempts"
	"hookline/internal/domain"
	"hookline/internal/signing"
)

type Result struct {
	Success            bool
	StatusCode         *int
	BodySnippet, Error string
	Duration           time.Duration
	SentHeaders        map[string]string
}
type Sender interface {
	Send(context.Context, domain.Endpoint, domain.Event, domain.MessageID, time.Time) Result
}
type HTTPSender struct {
	client  *http.Client
	clock   domain.Clock
	timeout time.Duration
	max     int64
	snippet int
	agent   string
}

func New(c *http.Client, k domain.Clock, t time.Duration, max int64, snippet int, agent string) *HTTPSender {
	return &HTTPSender{c, k, t, max, snippet, agent}
}

func (s *HTTPSender) Send(c context.Context, ep domain.Endpoint, ev domain.Event, id domain.MessageID, now time.Time) Result {
	start := s.clock.Now()
	out := Result{}
	ctx, cancel := context.WithTimeout(c, s.timeout)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(ev.Payload))
	if e != nil {
		out.Error = e.Error()
		return out
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", s.agent)
	r.Header.Set("X-Hookline-Id", string(id))
	r.Header.Set("X-Hookline-Event-Type", ev.Type)
	r.Header.Set("X-Hookline-Timestamp", strconv.FormatInt(now.Unix(), 10))
	r.Header.Set("X-Hookline-Signature", signing.Sign(ev.Payload, ep.Secret, now))
	h := map[string]string{}
	for k, v := range r.Header {
		h[k] = v[0]
	}
	out.SentHeaders = attempts.RedactHeaders(h)
	resp, e := s.client.Do(r)
	out.Duration = s.clock.Now().Sub(start)
	if e != nil {
		out.Error = e.Error()
		return out
	}
	defer func() { _ = resp.Body.Close() }()
	code := resp.StatusCode
	out.StatusCode = &code
	b, e := io.ReadAll(io.LimitReader(resp.Body, s.max))
	if e != nil {
		out.Error = fmt.Sprintf("read response: %v", e)
		return out
	}
	out.BodySnippet = attempts.TruncateUTF8(string(b), s.snippet)
	out.Success = code >= 200 && code < 300
	return out
}
