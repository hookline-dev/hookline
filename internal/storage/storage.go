package storage

import (
	"context"

	"hookline/internal/domain"
)

type IngestTx interface {
	InsertEvent(context.Context, domain.Event) error
	FindEventByIdempotencyKey(context.Context, domain.AppID, string) (domain.Event, error)
	ListActiveTargets(context.Context, domain.AppID) ([]domain.Target, error)
	InsertMessages(context.Context, []domain.Message) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
type IngestStore interface {
	BeginIngest(context.Context) (IngestTx, error)
}
