package domain

import "errors"

var (
	// ErrNotFound indicates that the requested resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicateIdemKey indicates an already accepted idempotency key.
	ErrDuplicateIdemKey = errors.New("duplicate idempotency key")
	// ErrEndpointDisabled indicates that delivery to an endpoint is disabled.
	ErrEndpointDisabled = errors.New("endpoint disabled")
	// ErrInvalidEventType indicates an invalid event type or pattern.
	ErrInvalidEventType = errors.New("invalid event type")
	// ErrInvalidSignature indicates a malformed or mismatched signature.
	ErrInvalidSignature = errors.New("invalid signature")
	// ErrSignatureExpired indicates a signature outside the accepted time window.
	ErrSignatureExpired = errors.New("signature timestamp out of tolerance")
	// ErrBreakerOpen indicates that the endpoint circuit breaker is open.
	ErrBreakerOpen = errors.New("circuit breaker is open")
	// ErrInvalidJSON indicates that a payload is not valid JSON.
	ErrInvalidJSON = errors.New("invalid json")
	// ErrPayloadTooLarge indicates that a body exceeds the configured limit.
	ErrPayloadTooLarge = errors.New("payload too large")
	// ErrInvalidInput indicates invalid request fields.
	ErrInvalidInput = errors.New("invalid input")
	// ErrUnauthorized indicates missing or invalid credentials.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrConflict indicates a resource state conflict.
	ErrConflict = errors.New("conflict")
)
