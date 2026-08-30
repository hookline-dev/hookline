package ratelimit

import (
	"context"
	"time"

	"hookline/internal/domain"
)

type repository interface {
	AllowRate(context.Context, domain.EndpointID, time.Time) (bool, time.Time, error)
}
type Limiter struct{ r repository }

func New(r repository) *Limiter { return &Limiter{r} }
func (l *Limiter) Allow(c context.Context, id domain.EndpointID, now time.Time) (bool, time.Time, error) {
	return l.r.AllowRate(c, id, now)
}
