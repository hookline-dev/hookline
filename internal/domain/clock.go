package domain

import "time"

// Clock provides the current time to business logic.
type Clock interface{ Now() time.Time }

// RealClock is the production UTC clock.
type RealClock struct{}

// Now returns the current UTC time.
func (RealClock) Now() time.Time { return time.Now().UTC() }
