package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/internal/domain"
	"hookline/internal/storage"
)

type Store struct{ pool *pgxpool.Pool }

func New(p *pgxpool.Pool) *Store              { return &Store{p} }
func (s *Store) Ping(c context.Context) error { return s.pool.Ping(c) }
func (s *Store) BeginIngest(c context.Context) (storage.IngestTx, error) {
	tx, e := s.pool.BeginTx(c, pgx.TxOptions{})
	if e != nil {
		return nil, fmt.Errorf("begin ingest: %w", e)
	}
	return &ingestTx{tx}, nil
}

type ingestTx struct{ tx pgx.Tx }

func (t *ingestTx) InsertEvent(c context.Context, e domain.Event) error {
	var idem any
	if e.IdemKey != "" {
		idem = e.IdemKey
		if _, x := t.tx.Exec(c, "SAVEPOINT hookline_idem"); x != nil {
			return x
		}
	}
	_, x := t.tx.Exec(c, `INSERT INTO events(id,app_id,event_type,payload,idem_key,received_at) VALUES($1::uuid,$2::uuid,$3,$4::jsonb,$5,$6)`, e.ID, e.AppID, e.Type, e.Payload, idem, e.ReceivedAt)
	if x != nil {
		var p *pgconn.PgError
		if errors.As(x, &p) && p.Code == "23505" && e.IdemKey != "" {
			_, _ = t.tx.Exec(c, "ROLLBACK TO SAVEPOINT hookline_idem")
			_, _ = t.tx.Exec(c, "RELEASE SAVEPOINT hookline_idem")
			return domain.ErrDuplicateIdemKey
		}
		if errors.As(x, &p) && p.Code == "23503" {
			return domain.ErrNotFound
		}
		return fmt.Errorf("insert event: %w", x)
	}
	if e.IdemKey != "" {
		_, x = t.tx.Exec(c, "RELEASE SAVEPOINT hookline_idem")
	}
	return x
}

func (t *ingestTx) FindEventByIdempotencyKey(c context.Context, a domain.AppID, k string) (domain.Event, error) {
	var e domain.Event
	x := t.tx.QueryRow(c, `SELECT id,app_id,event_type,payload,COALESCE(idem_key,''),received_at FROM events WHERE app_id=$1::uuid AND idem_key=$2`, a, k).Scan(&e.ID, &e.AppID, &e.Type, &e.Payload, &e.IdemKey, &e.ReceivedAt)
	return e, mapNF("find event", x)
}

func (t *ingestTx) ListActiveTargets(c context.Context, a domain.AppID) ([]domain.Target, error) {
	r, e := t.tx.Query(c, `SELECT e.id,array_agg(s.event_type ORDER BY s.event_type) FROM endpoints e JOIN subscriptions s ON s.endpoint_id=e.id WHERE e.app_id=$1::uuid AND e.status='active' GROUP BY e.id`, a)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var out []domain.Target
	for r.Next() {
		var v domain.Target
		if e = r.Scan(&v.EndpointID, &v.Patterns); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}

func (t *ingestTx) InsertMessages(c context.Context, ms []domain.Message) error {
	b := &pgx.Batch{}
	for _, m := range ms {
		b.Queue(`INSERT INTO messages(id,event_id,endpoint_id,status,attempt,next_attempt_at,replay_of,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,0,$5,$6::uuid,$7,$7)`, m.ID, m.EventID, m.EndpointID, m.Status, m.NextAttemptAt, m.ReplayOf, m.CreatedAt)
	}
	r := t.tx.SendBatch(c, b)
	defer func() { _ = r.Close() }()
	for range ms {
		if _, e := r.Exec(); e != nil {
			return fmt.Errorf("insert messages: %w", e)
		}
	}
	return nil
}
func (t *ingestTx) Commit(c context.Context) error { return t.tx.Commit(c) }
func (t *ingestTx) Rollback(c context.Context) error {
	e := t.tx.Rollback(c)
	if errors.Is(e, pgx.ErrTxClosed) {
		return nil
	}
	return e
}

func (s *Store) CreateApp(c context.Context, a domain.App, h, g string) error {
	_, e := s.pool.Exec(c, `INSERT INTO apps(id,name,api_key_hash,github_webhook_secret,created_at) VALUES($1::uuid,$2,$3,$4,$5)`, a.ID, a.Name, h, g, a.CreatedAt)
	return wrap("create app", e)
}

func (s *Store) ListApps(c context.Context) ([]domain.App, error) {
	r, e := s.pool.Query(c, `SELECT id,name,created_at FROM apps ORDER BY created_at DESC,id DESC`)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []domain.App{}
	for r.Next() {
		var a domain.App
		if e = r.Scan(&a.ID, &a.Name, &a.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, r.Err()
}

func (s *Store) APIKeyExists(c context.Context, h string) (bool, error) {
	var v bool
	e := s.pool.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM apps WHERE api_key_hash=$1)`, h).Scan(&v)
	return v, e
}

func (s *Store) GetGitHubSecret(c context.Context, a domain.AppID) (string, error) {
	var v string
	e := s.pool.QueryRow(c, `SELECT github_webhook_secret FROM apps WHERE id=$1::uuid`, a).Scan(&v)
	return v, mapNF("github secret", e)
}

func (s *Store) CreateEndpoint(c context.Context, v domain.Endpoint) error {
	st := "active"
	if v.Disabled {
		st = "disabled"
	}
	_, e := s.pool.Exec(c, `INSERT INTO endpoints(id,app_id,url,secret,status,breaker_state,breaker_failures,breaker_open_duration,rate_limit_rps,created_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8::bigint*interval '1 microsecond',$9,$10)`, v.ID, v.AppID, v.URL, v.Secret, st, v.BreakerState, v.BreakerFailures, v.BreakerOpenDuration.Microseconds(), v.RateLimitRPS, v.CreatedAt)
	if p := pgError(e); p != nil && p.Code == "23503" {
		return domain.ErrNotFound
	}
	return wrap("create endpoint", e)
}

const endpointSelect = `SELECT id,app_id,url,secret,status,breaker_state,breaker_failures,breaker_opened_at,EXTRACT(EPOCH FROM breaker_open_duration)::float8,rate_limit_rps,created_at FROM endpoints`

type scanner interface{ Scan(...any) error }

func scanEndpoint(r scanner) (domain.Endpoint, error) {
	var v domain.Endpoint
	var st string
	var d float64
	e := r.Scan(&v.ID, &v.AppID, &v.URL, &v.Secret, &st, &v.BreakerState, &v.BreakerFailures, &v.BreakerOpenedAt, &d, &v.RateLimitRPS, &v.CreatedAt)
	v.Disabled = st == "disabled"
	v.BreakerOpenDuration = time.Duration(d * float64(time.Second))
	return v, e
}

func (s *Store) ListEndpoints(c context.Context, a domain.AppID) ([]domain.Endpoint, error) {
	r, e := s.pool.Query(c, endpointSelect+` WHERE app_id=$1::uuid ORDER BY created_at DESC`, a)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []domain.Endpoint{}
	for r.Next() {
		v, e := scanEndpoint(r)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}

func (s *Store) GetEndpoint(c context.Context, id domain.EndpointID) (domain.Endpoint, error) {
	v, e := scanEndpoint(s.pool.QueryRow(c, endpointSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(e, pgx.ErrNoRows) {
		e = domain.ErrNotFound
	}
	return v, e
}

func (s *Store) DisableEndpoint(c context.Context, id domain.EndpointID) error {
	return affected(s.pool.Exec(c, `UPDATE endpoints SET status='disabled' WHERE id=$1::uuid`, id))
}

func (s *Store) ResetBreaker(c context.Context, id domain.EndpointID, d time.Duration) error {
	return affected(s.pool.Exec(c, `UPDATE endpoints SET breaker_state='closed',breaker_failures=0,breaker_opened_at=NULL,breaker_probe_in_flight=false,breaker_open_duration=$2::bigint*interval '1 microsecond' WHERE id=$1::uuid`, id, d.Microseconds()))
}

func (s *Store) CreateSubscription(c context.Context, v domain.Subscription) error {
	_, e := s.pool.Exec(c, `INSERT INTO subscriptions(id,endpoint_id,event_type,created_at) VALUES($1::uuid,$2::uuid,$3,$4)`, v.ID, v.EndpointID, v.EventType, v.CreatedAt)
	if p := pgError(e); p != nil {
		if p.Code == "23503" {
			return domain.ErrNotFound
		}
		if p.Code == "23505" {
			return domain.ErrConflict
		}
	}
	return wrap("create subscription", e)
}

func (s *Store) DeleteSubscription(c context.Context, id domain.SubscriptionID) error {
	return affected(s.pool.Exec(c, `DELETE FROM subscriptions WHERE id=$1::uuid`, id))
}

func (s *Store) ListEvents(c context.Context, f domain.EventFilter) ([]domain.Event, error) {
	q := `SELECT id,app_id,event_type,payload,COALESCE(idem_key,''),received_at FROM events WHERE true`
	a := []any{}
	if f.Type != "" {
		a = append(a, f.Type)
		q += fmt.Sprintf(" AND event_type=$%d", len(a))
	}
	if f.Before != nil {
		a = append(a, f.Before.CreatedAt, f.Before.ID)
		q += fmt.Sprintf(" AND (received_at,id)<($%d,$%d::uuid)", len(a)-1, len(a))
	}
	a = append(a, f.Limit)
	q += fmt.Sprintf(" ORDER BY received_at DESC,id DESC LIMIT $%d", len(a))
	r, e := s.pool.Query(c, q, a...)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []domain.Event{}
	for r.Next() {
		var v domain.Event
		if e = r.Scan(&v.ID, &v.AppID, &v.Type, &v.Payload, &v.IdemKey, &v.ReceivedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}

func scanMessage(r scanner, v *domain.Message) error {
	return r.Scan(&v.ID, &v.EventID, &v.EndpointID, &v.Status, &v.Attempt, &v.NextAttemptAt, &v.LockedUntil, &v.LockedBy, &v.ReplayOf, &v.CreatedAt, &v.UpdatedAt)
}

func (s *Store) ListMessages(c context.Context, f domain.MessageFilter) ([]domain.Message, error) {
	q := `SELECT id,event_id,endpoint_id,status,attempt,next_attempt_at,locked_until,locked_by,replay_of,created_at,updated_at FROM messages WHERE true`
	a := []any{}
	if f.Status != "" {
		a = append(a, f.Status)
		q += fmt.Sprintf(" AND status=$%d", len(a))
	}
	if f.EndpointID != "" {
		a = append(a, f.EndpointID)
		q += fmt.Sprintf(" AND endpoint_id=$%d::uuid", len(a))
	}
	if f.Before != nil {
		a = append(a, f.Before.CreatedAt, f.Before.ID)
		q += fmt.Sprintf(" AND (created_at,id)<($%d,$%d::uuid)", len(a)-1, len(a))
	}
	a = append(a, f.Limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(a))
	r, e := s.pool.Query(c, q, a...)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []domain.Message{}
	for r.Next() {
		var v domain.Message
		if e = scanMessage(r, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}

func (s *Store) GetMessageDetail(c context.Context, id domain.MessageID) (domain.MessageDetail, error) {
	var d domain.MessageDetail
	var st string
	var sec float64
	e := s.pool.QueryRow(c, `SELECT m.id,m.event_id,m.endpoint_id,m.status,m.attempt,m.next_attempt_at,m.locked_until,m.locked_by,m.replay_of,m.created_at,m.updated_at,e.id,e.app_id,e.event_type,e.payload,COALESCE(e.idem_key,''),e.received_at,p.id,p.app_id,p.url,p.secret,p.status,p.breaker_state,p.breaker_failures,p.breaker_opened_at,EXTRACT(EPOCH FROM p.breaker_open_duration)::float8,p.rate_limit_rps,p.created_at FROM messages m JOIN events e ON e.id=m.event_id JOIN endpoints p ON p.id=m.endpoint_id WHERE m.id=$1::uuid`, id).Scan(&d.Message.ID, &d.Message.EventID, &d.Message.EndpointID, &d.Message.Status, &d.Message.Attempt, &d.Message.NextAttemptAt, &d.Message.LockedUntil, &d.Message.LockedBy, &d.Message.ReplayOf, &d.Message.CreatedAt, &d.Message.UpdatedAt, &d.Event.ID, &d.Event.AppID, &d.Event.Type, &d.Event.Payload, &d.Event.IdemKey, &d.Event.ReceivedAt, &d.Endpoint.ID, &d.Endpoint.AppID, &d.Endpoint.URL, &d.Endpoint.Secret, &st, &d.Endpoint.BreakerState, &d.Endpoint.BreakerFailures, &d.Endpoint.BreakerOpenedAt, &sec, &d.Endpoint.RateLimitRPS, &d.Endpoint.CreatedAt)
	if e != nil {
		return d, mapNF("message detail", e)
	}
	d.Endpoint.Disabled = st == "disabled"
	d.Endpoint.BreakerOpenDuration = time.Duration(sec * float64(time.Second))
	d.Attempts, e = s.ListAttemptsByMessage(c, id)
	return d, e
}

func (s *Store) ListAttemptsByMessage(c context.Context, id domain.MessageID) ([]domain.Attempt, error) {
	r, e := s.pool.Query(c, `SELECT attempt_no,request_headers,response_code,response_snippet,error,duration_ms,created_at FROM attempts WHERE message_id=$1::uuid ORDER BY attempt_no`, id)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []domain.Attempt{}
	for r.Next() {
		var v domain.Attempt
		var h []byte
		var sn, er *string
		var ms int64
		v.MessageID = id
		if e = r.Scan(&v.AttemptNo, &h, &v.ResponseCode, &sn, &er, &ms, &v.CreatedAt); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(h, &v.RequestHeaders)
		if sn != nil {
			v.ResponseSnippet = *sn
		}
		if er != nil {
			v.Error = *er
		}
		v.Duration = time.Duration(ms) * time.Millisecond
		out = append(out, v)
	}
	return out, r.Err()
}

func (s *Store) ReplayMessage(c context.Context, id, n domain.MessageID, now time.Time) (domain.Message, error) {
	tx, e := s.pool.Begin(c)
	if e != nil {
		return domain.Message{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	var v domain.Message
	e = tx.QueryRow(c, `SELECT event_id,endpoint_id FROM messages WHERE id=$1::uuid FOR SHARE`, id).Scan(&v.EventID, &v.EndpointID)
	if e != nil {
		return v, mapNF("replay", e)
	}
	v.ID = n
	v.Status = domain.StatusPending
	v.NextAttemptAt = now
	v.CreatedAt = now
	v.UpdatedAt = now
	v.ReplayOf = &id
	_, e = tx.Exec(c, `INSERT INTO messages(id,event_id,endpoint_id,status,attempt,next_attempt_at,replay_of,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,'pending',0,$4,$5::uuid,$4,$4)`, n, v.EventID, v.EndpointID, now, id)
	if e == nil {
		e = tx.Commit(c)
	}
	return v, e
}

func (s *Store) TryHalfOpen(c context.Context, id domain.EndpointID, now time.Time) (bool, error) {
	t, e := s.pool.Exec(c, `UPDATE endpoints SET breaker_state='half_open',breaker_probe_in_flight=true WHERE id=$1::uuid AND breaker_state='open' AND breaker_probe_in_flight=false AND breaker_opened_at+breaker_open_duration<=$2`, id, now)
	return t.RowsAffected() == 1, e
}

func (s *Store) RecordBreakerSuccess(c context.Context, id domain.EndpointID, d time.Duration) error {
	_, e := s.pool.Exec(c, `UPDATE endpoints SET breaker_state='closed',breaker_failures=0,breaker_opened_at=NULL,breaker_probe_in_flight=false,breaker_open_duration=$2::bigint*interval '1 microsecond' WHERE id=$1::uuid`, id, d.Microseconds())
	return e
}

func (s *Store) RecordBreakerFailure(c context.Context, id domain.EndpointID, now time.Time, threshold int, max time.Duration) error {
	_, e := s.pool.Exec(c, `UPDATE endpoints SET breaker_failures=breaker_failures+1,breaker_opened_at=CASE WHEN breaker_state='half_open' OR (breaker_state='closed' AND breaker_failures+1 >= $3) THEN $2 ELSE breaker_opened_at END,breaker_open_duration=CASE WHEN breaker_state='half_open' THEN LEAST(breaker_open_duration*2,$4::bigint*interval '1 microsecond') ELSE breaker_open_duration END,breaker_state=CASE WHEN breaker_state='half_open' OR (breaker_state='closed' AND breaker_failures+1 >= $3) THEN 'open' ELSE breaker_state END,breaker_probe_in_flight=false WHERE id=$1::uuid`, id, now, threshold, max.Microseconds())
	return e
}

func (s *Store) AllowRate(c context.Context, id domain.EndpointID, now time.Time) (bool, time.Time, error) {
	var w time.Time
	e := s.pool.QueryRow(c, `UPDATE endpoints SET rate_window_started_at=CASE WHEN rate_window_started_at IS NULL OR rate_window_started_at <= $2::timestamptz-interval '1 second' THEN $2 ELSE rate_window_started_at END,rate_window_count=CASE WHEN rate_window_started_at IS NULL OR rate_window_started_at <= $2::timestamptz-interval '1 second' THEN 1 ELSE rate_window_count+1 END WHERE id=$1::uuid AND status='active' AND (rate_window_started_at IS NULL OR rate_window_started_at <= $2::timestamptz-interval '1 second' OR rate_window_count<rate_limit_rps) RETURNING rate_window_started_at`, id, now).Scan(&w)
	if e == nil {
		return true, time.Time{}, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return false, time.Time{}, e
	}
	e = s.pool.QueryRow(c, `SELECT rate_window_started_at FROM endpoints WHERE id=$1::uuid`, id).Scan(&w)
	return false, w.Add(time.Second), mapNF("rate", e)
}

func (s *Store) QueueStats(c context.Context, now time.Time) (p, i, d int64, o float64, e error) {
	e = s.pool.QueryRow(c, `SELECT count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='in_flight'),count(*) FILTER(WHERE status='dead'),COALESCE(EXTRACT(EPOCH FROM($1-MIN(created_at) FILTER(WHERE status='pending'))),0) FROM messages`, now).Scan(&p, &i, &d, &o)
	return
}

func affected(t pgconn.CommandTag, e error) error {
	if e != nil {
		return e
	}
	if t.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func pgError(e error) *pgconn.PgError {
	var p *pgconn.PgError
	if errors.As(e, &p) {
		return p
	}
	return nil
}

func mapNF(op string, e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return wrap(op, e)
}

func wrap(op string, e error) error {
	if e == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, e)
}
