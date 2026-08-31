//go:build integration

package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hookline/internal/domain"
	"hookline/internal/ingest"
	storepostgres "hookline/internal/storage/postgres"
	"hookline/internal/testdb"
)

func TestAcceptRequiredScenarios(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t)
	store := storepostgres.New(db)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()

	newApp := func(t *testing.T) domain.AppID {
		t.Helper()
		id := mustID(t)
		appID := domain.AppID(id)
		if err := store.CreateApp(ctx, domain.App{ID: appID, Name: "ingest-" + id, CreatedAt: now}, "hash-"+id, "github-webhook-secret"); err != nil {
			t.Fatal(err)
		}
		return appID
	}
	addTarget := func(t *testing.T, appID domain.AppID, disabled bool, pattern string) domain.EndpointID {
		t.Helper()
		endpointID := domain.EndpointID(mustID(t))
		if err := store.CreateEndpoint(ctx, domain.Endpoint{
			ID:                  endpointID,
			AppID:               appID,
			URL:                 "https://example.test/hook/" + string(endpointID),
			Secret:              "endpoint-secret",
			Disabled:            disabled,
			BreakerState:        domain.BreakerClosed,
			BreakerOpenDuration: 5 * time.Minute,
			RateLimitRPS:        5,
			CreatedAt:           now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateSubscription(ctx, domain.Subscription{ID: domain.SubscriptionID(mustID(t)), EndpointID: endpointID, EventType: pattern, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		return endpointID
	}
	messageCount := func(t *testing.T, appID domain.AppID) int {
		t.Helper()
		messages, err := store.ListMessages(ctx, domain.MessageFilter{AppID: appID, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		return len(messages)
	}
	eventCount := func(t *testing.T, appID domain.AppID) int {
		t.Helper()
		events, err := store.ListEvents(ctx, domain.EventFilter{AppID: appID, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		return len(events)
	}

	t.Run("three matching endpoints create three messages", func(t *testing.T) {
		appID := newApp(t)
		addTarget(t, appID, false, "order.created")
		addTarget(t, appID, false, "order.*")
		addTarget(t, appID, false, "*")
		result, err := ingest.New(store, 1024).AcceptDetailed(ctx, ingest.Request{AppID: appID, EventType: "order.created", Payload: []byte(`{"order":42}`), IdemKey: "three-targets"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if result.MessagesCreated != 3 || result.Duplicate || messageCount(t, appID) != 3 {
			t.Fatalf("unexpected result: %#v, stored messages=%d", result, messageCount(t, appID))
		}
	})

	t.Run("event without subscriptions is stored", func(t *testing.T) {
		appID := newApp(t)
		result, err := ingest.New(store, 1024).AcceptDetailed(ctx, ingest.Request{AppID: appID, EventType: "order.created", Payload: []byte(`{}`)}, now)
		if err != nil {
			t.Fatal(err)
		}
		if result.MessagesCreated != 0 || eventCount(t, appID) != 1 || messageCount(t, appID) != 0 {
			t.Fatalf("unexpected result: %#v, events=%d messages=%d", result, eventCount(t, appID), messageCount(t, appID))
		}
	})

	t.Run("disabled endpoint receives no message", func(t *testing.T) {
		appID := newApp(t)
		addTarget(t, appID, true, "*")
		result, err := ingest.New(store, 1024).AcceptDetailed(ctx, ingest.Request{AppID: appID, EventType: "order.created", Payload: []byte(`{}`)}, now)
		if err != nil {
			t.Fatal(err)
		}
		if result.MessagesCreated != 0 || messageCount(t, appID) != 0 {
			t.Fatalf("disabled endpoint received a message: %#v", result)
		}
	})

	for _, tc := range []struct {
		name    string
		max     int64
		payload string
		want    error
	}{
		{name: "invalid JSON writes nothing", max: 1024, payload: `{`, want: domain.ErrInvalidJSON},
		{name: "oversized body writes nothing", max: 1, payload: `{}`, want: domain.ErrPayloadTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			appID := newApp(t)
			_, err := ingest.New(store, tc.max).AcceptDetailed(ctx, ingest.Request{AppID: appID, EventType: "order.created", Payload: []byte(tc.payload)}, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if events := eventCount(t, appID); events != 0 {
				t.Fatalf("stored %d events after rejected request", events)
			}
		})
	}

	t.Run("duplicate idempotency key reuses event", func(t *testing.T) {
		appID := newApp(t)
		addTarget(t, appID, false, "*")
		service := ingest.New(store, 1024)
		request := ingest.Request{AppID: appID, EventType: "order.created", Payload: []byte(`{"order":42}`), IdemKey: "duplicate-key"}
		first, err := service.AcceptDetailed(ctx, request, now)
		if err != nil {
			t.Fatal(err)
		}
		request.Payload = []byte(`{"order":99}`)
		second, err := service.AcceptDetailed(ctx, request, now.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if first.Duplicate || !second.Duplicate || second.MessagesCreated != 0 || first.Event.ID != second.Event.ID {
			t.Fatalf("first=%#v second=%#v", first, second)
		}
		if events, messages := eventCount(t, appID), messageCount(t, appID); events != 1 || messages != 1 {
			t.Fatalf("events=%d messages=%d, want 1 and 1", events, messages)
		}
	})
}

func mustID(t *testing.T) string {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(fmt.Errorf("generate id: %w", err))
	}
	return id
}
