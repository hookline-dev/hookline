package domain

import (
	"encoding/json"
	"time"
)

// AppID identifies an application.
type AppID string

// EndpointID identifies a webhook receiver.
type EndpointID string

// SubscriptionID identifies an event subscription.
type SubscriptionID string

// EventID identifies an accepted event.
type EventID string

// MessageID identifies one event-to-endpoint delivery.
type MessageID string

// MessageStatus is the lifecycle state of a delivery message.
type MessageStatus string

// BreakerState is the persisted circuit-breaker state.
type BreakerState string

const (
	// StatusPending marks a message available for claiming.
	StatusPending MessageStatus = "pending"
	// StatusInFlight marks a message leased to a worker.
	StatusInFlight MessageStatus = "in_flight"
	// StatusDelivered marks a successfully delivered message.
	StatusDelivered MessageStatus = "delivered"
	// StatusDead marks a message moved to the dead-letter queue.
	StatusDead MessageStatus = "dead"
	// BreakerClosed allows normal delivery traffic.
	BreakerClosed BreakerState = "closed"
	// BreakerOpen temporarily blocks delivery traffic.
	BreakerOpen BreakerState = "open"
	// BreakerHalfOpen allows a single recovery probe.
	BreakerHalfOpen BreakerState = "half_open"
)

// App groups endpoints and events under one API key.
type App struct {
	ID        AppID     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// Endpoint describes a webhook receiver and its delivery policy.
type Endpoint struct {
	ID                  EndpointID    `json:"id"`
	AppID               AppID         `json:"appId"`
	URL                 string        `json:"url"`
	Secret              string        `json:"-"`
	Disabled            bool          `json:"-"`
	BreakerState        BreakerState  `json:"breakerState"`
	BreakerFailures     int           `json:"breakerFailures"`
	BreakerOpenedAt     *time.Time    `json:"breakerOpenedAt,omitempty"`
	BreakerOpenDuration time.Duration `json:"-"`
	RateLimitRPS        int           `json:"rateLimitRps"`
	CreatedAt           time.Time     `json:"createdAt"`
}

// Subscription maps an endpoint to an event type pattern.
type Subscription struct {
	ID         SubscriptionID `json:"id"`
	EndpointID EndpointID     `json:"endpointId"`
	EventType  string         `json:"eventType"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// Event is an immutable accepted source event.
type Event struct {
	ID         EventID         `json:"id"`
	AppID      AppID           `json:"appId"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	IdemKey    string          `json:"idempotencyKey,omitempty"`
	ReceivedAt time.Time       `json:"receivedAt"`
}

// Message is one queued delivery of an event to an endpoint.
type Message struct {
	ID            MessageID     `json:"id"`
	EventID       EventID       `json:"eventId"`
	EndpointID    EndpointID    `json:"endpointId"`
	Status        MessageStatus `json:"status"`
	Attempt       int           `json:"attempt"`
	NextAttemptAt time.Time     `json:"nextAttemptAt"`
	LockedUntil   *time.Time    `json:"-"`
	LockedBy      *string       `json:"-"`
	ReplayOf      *MessageID    `json:"replayOf,omitempty"`
	CreatedAt     time.Time     `json:"createdAt"`
	UpdatedAt     time.Time     `json:"updatedAt"`
}

// Attempt records one delivery request and its result.
type Attempt struct {
	MessageID       MessageID
	AttemptNo       int               `json:"attemptNo"`
	RequestHeaders  map[string]string `json:"-"`
	ResponseCode    *int              `json:"responseCode"`
	ResponseSnippet string            `json:"snippet,omitempty"`
	Error           string            `json:"error,omitempty"`
	Duration        time.Duration     `json:"-"`
	CreatedAt       time.Time         `json:"createdAt"`
}

// Target combines an endpoint with active subscription patterns.
type Target struct {
	EndpointID EndpointID
	Patterns   []string
}

// MessageDetail contains the full delivery audit view.
type MessageDetail struct {
	Message  Message
	Event    Event
	Endpoint Endpoint
	Attempts []Attempt
}

// Cursor is a stable keyset-pagination position.
type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

// EventFilter selects a page of events.
type EventFilter struct {
	AppID  AppID
	Type   string
	Before *Cursor
	Limit  int
}

// MessageFilter selects a page of delivery messages.
type MessageFilter struct {
	AppID      AppID
	Status     MessageStatus
	EndpointID EndpointID
	Before     *Cursor
	Limit      int
}
