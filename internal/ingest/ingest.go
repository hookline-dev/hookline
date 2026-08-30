package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"hookline/internal/domain"
	"hookline/internal/matcher"
	"hookline/internal/storage"
)

type Request struct {
	AppID     domain.AppID
	EventType string
	Payload   []byte
	IdemKey   string
}
type Result struct {
	Event           domain.Event
	MessagesCreated int
	Duplicate       bool
}
type Service struct {
	store storage.IngestStore
	max   int64
}
type DetailedService interface {
	AcceptDetailed(context.Context, Request, time.Time) (Result, error)
}

func New(s storage.IngestStore, max int64) *Service { return &Service{s, max} }
func (s *Service) Accept(c context.Context, a domain.AppID, t string, p []byte, k string, now time.Time) (domain.Event, int, bool, error) {
	r, e := s.AcceptDetailed(c, Request{a, t, p, k}, now)
	return r.Event, r.MessagesCreated, r.Duplicate, e
}

func (s *Service) AcceptDetailed(c context.Context, r Request, now time.Time) (Result, error) {
	if !domain.ValidID(string(r.AppID)) || r.EventType == "" || len(r.EventType) > 255 {
		return Result{}, domain.ErrInvalidEventType
	}
	if int64(len(r.Payload)) > s.max {
		return Result{}, domain.ErrPayloadTooLarge
	}
	if !json.Valid(r.Payload) {
		return Result{}, domain.ErrInvalidJSON
	}
	id, e := domain.NewID()
	if e != nil {
		return Result{}, e
	}
	ev := domain.Event{ID: domain.EventID(id), AppID: r.AppID, Type: r.EventType, Payload: append([]byte(nil), r.Payload...), IdemKey: r.IdemKey, ReceivedAt: now}
	tx, e := s.store.BeginIngest(c)
	if e != nil {
		return Result{}, e
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(c)) }()
	if e = tx.InsertEvent(c, ev); errors.Is(e, domain.ErrDuplicateIdemKey) {
		old, x := tx.FindEventByIdempotencyKey(c, r.AppID, r.IdemKey)
		if x != nil {
			return Result{}, x
		}
		if x = tx.Commit(c); x != nil {
			return Result{}, x
		}
		return Result{Event: old, Duplicate: true}, nil
	} else if e != nil {
		return Result{}, e
	}
	targets, e := tx.ListActiveTargets(c, r.AppID)
	if e != nil {
		return Result{}, e
	}
	var ms []domain.Message
	for _, v := range targets {
		if !matcher.MatchAny(r.EventType, v.Patterns) {
			continue
		}
		x, e := domain.NewID()
		if e != nil {
			return Result{}, e
		}
		ms = append(ms, domain.Message{ID: domain.MessageID(x), EventID: ev.ID, EndpointID: v.EndpointID, Status: domain.StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now})
	}
	if e = tx.InsertMessages(c, ms); e != nil {
		return Result{}, e
	}
	if e = tx.Commit(c); e != nil {
		return Result{}, e
	}
	return Result{Event: ev, MessagesCreated: len(ms)}, nil
}
