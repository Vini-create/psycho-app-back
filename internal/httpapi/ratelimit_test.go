package httpapi

import (
	"testing"
	"time"
)

func TestFixedWindowLimiter(t *testing.T) {
	limiter := newFixedWindowLimiter(2, time.Minute)
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("client") || !limiter.Allow("client") {
		t.Fatal("Allow() rejected a request inside the limit")
	}
	if limiter.Allow("client") {
		t.Fatal("Allow() accepted a request above the limit")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow("client") {
		t.Fatal("Allow() did not reset after the window")
	}
}
