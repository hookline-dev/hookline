package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/internal/attempts"
	"hookline/internal/domain"
)

type Queue interface {
	Claim(context.Context, string, int, time.Time, time.Duration) ([]ClaimedMessage, error)
	Ack(context.Context, domain.MessageID, domain.Attempt) error
	Nack(context.Context, domain.MessageID, domain.Attempt, time.Time, int) error
	Release(context.Context, domain.MessageID, time.Time, time.Time) error
	Reap(context.Context, time.Time) (int, error)
}
type ClaimedMessage struct {
	Message  domain.Message
	Event    domain.Event
	Endpoint domain.Endpoint
}
type PostgresQueue struct{ pool *pgxpool.Pool }

func New(p *pgxpool.Pool) *PostgresQueue { return &PostgresQueue{p} }
func (q *PostgresQueue) Claim(c context.Context, w string, n int, now time.Time, lease time.Duration) ([]ClaimedMessage, error) {
	if n <= 0 {
		return nil, nil
	}
	r, e := q.pool.Query(c, `WITH picked AS(SELECT m.id FROM messages m JOIN endpoints p ON p.id=m.endpoint_id WHERE m.status='pending' AND m.next_attempt_at<=$1 AND p.status='active' ORDER BY m.next_attempt_at,m.id FOR UPDATE OF m SKIP LOCKED LIMIT $4) UPDATE messages m SET status='in_flight',locked_until=$2,locked_by=$3,updated_at=$1 FROM picked x,events e,endpoints p WHERE m.id=x.id AND e.id=m.event_id AND p.id=m.endpoint_id RETURNING m.id,m.event_id,m.endpoint_id,m.status,m.attempt,m.next_attempt_at,m.locked_until,m.locked_by,m.replay_of,m.created_at,m.updated_at,e.id,e.app_id,e.event_type,e.payload,COALESCE(e.idem_key,''),e.received_at,p.id,p.app_id,p.url,p.secret,p.status,p.breaker_state,p.breaker_failures,p.breaker_opened_at,EXTRACT(EPOCH FROM p.breaker_open_duration)::float8,p.rate_limit_rps,p.created_at`, now, now.Add(lease), w, n)
	if e != nil {
		return nil, fmt.Errorf("claim: %w", e)
	}
	defer r.Close()
	out := make([]ClaimedMessage, 0, n)
	for r.Next() {
		var v ClaimedMessage
		var st string
		var d float64
		e = r.Scan(&v.Message.ID, &v.Message.EventID, &v.Message.EndpointID, &v.Message.Status, &v.Message.Attempt, &v.Message.NextAttemptAt, &v.Message.LockedUntil, &v.Message.LockedBy, &v.Message.ReplayOf, &v.Message.CreatedAt, &v.Message.UpdatedAt, &v.Event.ID, &v.Event.AppID, &v.Event.Type, &v.Event.Payload, &v.Event.IdemKey, &v.Event.ReceivedAt, &v.Endpoint.ID, &v.Endpoint.AppID, &v.Endpoint.URL, &v.Endpoint.Secret, &st, &v.Endpoint.BreakerState, &v.Endpoint.BreakerFailures, &v.Endpoint.BreakerOpenedAt, &d, &v.Endpoint.RateLimitRPS, &v.Endpoint.CreatedAt)
		if e != nil {
			return nil, e
		}
		v.Endpoint.Disabled = st == "disabled"
		v.Endpoint.BreakerOpenDuration = time.Duration(d * float64(time.Second))
		out = append(out, v)
	}
	return out, r.Err()
}

func (q *PostgresQueue) Ack(c context.Context, id domain.MessageID, a domain.Attempt) error {
	return q.complete(c, id, a, func(c context.Context, t pgx.Tx) (bool, error) {
		tag, e := t.Exec(c, `UPDATE messages SET status='delivered',locked_until=NULL,locked_by=NULL,updated_at=$2 WHERE id=$1::uuid AND status='in_flight'`, id, a.CreatedAt)
		return tag.RowsAffected() == 1, e
	})
}

func (q *PostgresQueue) Nack(c context.Context, id domain.MessageID, a domain.Attempt, next time.Time, max int) error {
	return q.complete(c, id, a, func(c context.Context, t pgx.Tx) (bool, error) {
		tag, e := t.Exec(c, `UPDATE messages SET attempt=attempt+1,status=CASE WHEN attempt+1 >= $3 THEN 'dead' ELSE 'pending' END,next_attempt_at=$2,locked_until=NULL,locked_by=NULL,updated_at=$4 WHERE id=$1::uuid AND status='in_flight'`, id, next, max, a.CreatedAt)
		return tag.RowsAffected() == 1, e
	})
}

func (q *PostgresQueue) complete(c context.Context, id domain.MessageID, a domain.Attempt, update func(context.Context, pgx.Tx) (bool, error)) error {
	t, e := q.pool.Begin(c)
	if e != nil {
		return e
	}
	defer func() { _ = t.Rollback(c) }()
	ok, e := update(c, t)
	if e != nil {
		return e
	}
	if !ok {
		return nil
	}
	h, e := json.Marshal(attempts.RedactHeaders(a.RequestHeaders))
	if e != nil {
		return e
	}
	var code any
	if a.ResponseCode != nil {
		code = *a.ResponseCode
	}
	_, e = t.Exec(c, `INSERT INTO attempts(message_id,attempt_no,request_headers,response_code,response_snippet,error,duration_ms,created_at) VALUES($1::uuid,$2,$3::jsonb,$4,$5,$6,$7,$8)`, id, a.AttemptNo, h, code, nullable(a.ResponseSnippet), nullable(a.Error), a.Duration.Milliseconds(), a.CreatedAt)
	if e != nil {
		return e
	}
	return t.Commit(c)
}

func (q *PostgresQueue) Release(c context.Context, id domain.MessageID, next, now time.Time) error {
	_, e := q.pool.Exec(c, `UPDATE messages SET status='pending',next_attempt_at=$2,locked_until=NULL,locked_by=NULL,updated_at=$3 WHERE id=$1::uuid AND status='in_flight'`, id, next, now)
	return e
}

func (q *PostgresQueue) Reap(c context.Context, now time.Time) (int, error) {
	t, e := q.pool.Exec(c, `UPDATE messages SET status='pending',locked_until=NULL,locked_by=NULL,updated_at=$1 WHERE status='in_flight' AND locked_until<$1`, now)
	return int(t.RowsAffected()), e
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
