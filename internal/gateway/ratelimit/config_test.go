package ratelimit

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigUsesSafeDefaultsAndRejectsInvalidValues(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	t.Setenv("RATE_LIMIT_KEY_SECRET", strings.Repeat("s", 32))
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Policies[LoginAccount] != (Policy{Capacity: 5, Window: 15 * time.Minute}) ||
		config.Policies[OfferResponse] != (Policy{Capacity: 30, Window: time.Minute}) {
		t.Fatalf("unexpected default policies: %+v", config.Policies)
	}

	t.Setenv("RATE_LIMIT_RIDE_REQUEST", "0/1m")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "RATE_LIMIT_RIDE_REQUEST") {
		t.Fatalf("invalid policy was accepted: %v", err)
	}
	t.Setenv("RATE_LIMIT_RIDE_REQUEST", "6/1m")
	t.Setenv("RATE_LIMIT_KEY_SECRET", "short")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("short key secret was accepted: %v", err)
	}
}

func TestParsePolicyBounds(t *testing.T) {
	for _, value := range []string{"ten/1m", "1", "1/500ms", "1/25h", "1000001/1m"} {
		if _, err := parsePolicy(value); err == nil {
			t.Errorf("parsePolicy(%q) unexpectedly succeeded", value)
		}
	}
	policy, err := parsePolicy("42/30s")
	if err != nil || policy != (Policy{Capacity: 42, Window: 30 * time.Second}) {
		t.Fatalf("valid policy parsed incorrectly: %+v, %v", policy, err)
	}
}
