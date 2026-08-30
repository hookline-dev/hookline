package breaker

import (
	"context"
	"time"

	"hookline/internal/domain"
)

type repository interface {
	GetEndpoint(context.Context, domain.EndpointID) (domain.Endpoint, error)
	TryHalfOpen(context.Context, domain.EndpointID, time.Time) (bool, error)
	RecordBreakerSuccess(context.Context, domain.EndpointID, time.Duration) error
	RecordBreakerFailure(context.Context, domain.EndpointID, time.Time, int, time.Duration) error
}
type Breaker interface {
	Allow(context.Context, domain.Endpoint, time.Time) (bool, time.Time)
	OnSuccess(context.Context, domain.EndpointID, time.Time) error
	OnFailure(context.Context, domain.EndpointID, time.Time) error
}
type Service struct {
	r             repository
	threshold     int
	duration, max time.Duration
}

func New(r repository, t int, d, m time.Duration) *Service { return &Service{r, t, d, m} }
func (s *Service) Allow(c context.Context, ep domain.Endpoint, now time.Time) (bool, time.Time) {
	v, e := s.r.GetEndpoint(c, ep.ID)
	if e != nil || v.Disabled {
		return false, now.Add(s.duration)
	}
	switch v.BreakerState {
	case domain.BreakerClosed:
		return true, time.Time{}
	case domain.BreakerOpen:
		if v.BreakerOpenedAt == nil {
			return false, now.Add(v.BreakerOpenDuration)
		}
		at := v.BreakerOpenedAt.Add(v.BreakerOpenDuration)
		if now.Before(at) {
			return false, at
		}
		ok, e := s.r.TryHalfOpen(c, ep.ID, now)
		if e == nil && ok {
			return true, time.Time{}
		}
		return false, now.Add(time.Second)
	default:
		return false, now.Add(time.Second)
	}
}

func (s *Service) OnSuccess(c context.Context, id domain.EndpointID, _ time.Time) error {
	return s.r.RecordBreakerSuccess(c, id, s.duration)
}

func (s *Service) OnFailure(c context.Context, id domain.EndpointID, now time.Time) error {
	return s.r.RecordBreakerFailure(c, id, now, s.threshold, s.max)
}
