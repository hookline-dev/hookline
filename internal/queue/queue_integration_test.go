//go:build integration

package queue_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/internal/domain"
	"hookline/internal/queue"
	"hookline/internal/testdb"
)

func TestQueueConcurrencyRetryReap(t *testing.T) {
	p := testdb.Open(t)
	now := time.Unix(1700000000, 0).UTC()
	ids := seedMessages(t, p, 200, now)
	q := queue.New(p)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[domain.MessageID]int{}
	responseStatus := 204
	for n := 0; n < 3; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for {
				v, e := q.Claim(context.Background(), fmt.Sprint(n), 10, now, time.Minute)
				if e != nil {
					t.Error(e)
					return
				}
				if len(v) == 0 {
					return
				}
				for _, x := range v {
					mu.Lock()
					seen[x.Message.ID]++
					mu.Unlock()
					if e = q.Ack(context.Background(), x.Message.ID, domain.Attempt{AttemptNo: 1, RequestHeaders: map[string]string{"Authorization": "secret", "X-Test": "yes"}, ResponseCode: &responseStatus, ResponseSnippet: "ok", CreatedAt: now, Duration: time.Millisecond}); e != nil {
						t.Error(e)
						return
					}
				}
			}
		}(n)
	}
	wg.Wait()
	if len(seen) != len(ids) {
		t.Fatalf("seen %d", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s=%d", id, n)
		}
	}
	future := seedMessages(t, p, 1, now.Add(time.Hour))[0]
	if got, claimErr := q.Claim(context.Background(), "early", 1, now, time.Minute); claimErr != nil || len(got) != 0 {
		t.Fatalf("future message claimed early: %#v %v", got, claimErr)
	}
	got, claimErr := q.Claim(context.Background(), "on-time", 1, now.Add(time.Hour), time.Minute)
	if claimErr != nil || len(got) != 1 || got[0].Message.ID != future {
		t.Fatalf("future message not claimed: %#v %v", got, claimErr)
	}
	if e := q.Ack(context.Background(), future, domain.Attempt{AttemptNo: 1, CreatedAt: now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if e := q.Ack(context.Background(), ids[0], domain.Attempt{AttemptNo: 2, CreatedAt: now}); e != nil {
		t.Fatalf("ack of an already completed message: %v", e)
	}
	id := seedMessages(t, p, 1, now)[0]
	v, e := q.Claim(context.Background(), "crash", 1, now, 10*time.Second)
	if e != nil || len(v) != 1 {
		t.Fatal(e)
	}
	if n, e := q.Reap(context.Background(), now.Add(11*time.Second)); e != nil || n != 1 {
		t.Fatalf("reap %d %v", n, e)
	}
	v, e = q.Claim(context.Background(), "retry", 1, now.Add(11*time.Second), time.Minute)
	if e != nil || len(v) != 1 {
		t.Fatal(e)
	}
	a := domain.Attempt{AttemptNo: 1, Error: "failed", CreatedAt: now.Add(11 * time.Second)}
	if e = q.Nack(context.Background(), id, a, now.Add(time.Minute), 2); e != nil {
		t.Fatal(e)
	}
	v, e = q.Claim(context.Background(), "dead", 1, now.Add(time.Minute), time.Minute)
	if e != nil || len(v) != 1 {
		t.Fatal(e)
	}
	a.AttemptNo = 2
	if e = q.Nack(context.Background(), id, a, now.Add(time.Hour), 2); e != nil {
		t.Fatal(e)
	}
	var status domain.MessageStatus
	if e = p.QueryRow(context.Background(), "SELECT status FROM messages WHERE id=$1::uuid", id).Scan(&status); e != nil || status != domain.StatusDead {
		t.Fatalf("%s %v", status, e)
	}
	if x, e := q.Claim(context.Background(), "zero", 0, now, time.Minute); e != nil || x != nil {
		t.Fatal("zero")
	}
	p.Close()
	if _, e := q.Claim(context.Background(), "closed", 1, now, time.Minute); e == nil {
		t.Error("claim on closed pool should fail")
	}
	if e := q.Ack(context.Background(), id, a); e == nil {
		t.Error("ack on closed pool should fail")
	}
	if e := q.Release(context.Background(), id, now, now); e == nil {
		t.Error("release on closed pool should fail")
	}
	if _, e := q.Reap(context.Background(), now); e == nil {
		t.Error("reap on closed pool should fail")
	}
}

func seedMessages(t testing.TB, p *pgxpool.Pool, n int, at time.Time) []domain.MessageID {
	t.Helper()
	id := func() string {
		x, e := domain.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	a, eid, ev := id(), id(), id()
	c := context.Background()
	if _, e := p.Exec(c, "INSERT INTO apps(id,name,api_key_hash,github_webhook_secret) VALUES($1::uuid,'x',$2,'g')", a, "h-"+a); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(c, "INSERT INTO endpoints(id,app_id,url,secret) VALUES($1::uuid,$2::uuid,'http://x','s')", eid, a); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(c, "INSERT INTO events(id,app_id,event_type,payload) VALUES($1::uuid,$2::uuid,'x','{}')", ev, a); e != nil {
		t.Fatal(e)
	}
	out := make([]domain.MessageID, n)
	for i := range out {
		out[i] = domain.MessageID(id())
		if _, e := p.Exec(c, "INSERT INTO messages(id,event_id,endpoint_id,next_attempt_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4)", out[i], ev, eid, at); e != nil {
			t.Fatal(e)
		}
	}
	return out
}
