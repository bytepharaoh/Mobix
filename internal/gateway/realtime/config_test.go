package realtime

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("SSE_MAX_CONNECTIONS", "40")
	t.Setenv("SSE_MAX_CONNECTIONS_PER_ACCOUNT", "3")
	t.Setenv("SSE_SUBSCRIBER_BUFFER", "7")
	t.Setenv("SSE_HEARTBEAT_INTERVAL", "2s")
	t.Setenv("SSE_WRITE_TIMEOUT", "3s")
	t.Setenv("SSE_MAX_LIFETIME", "20m")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxConnections != 40 || config.MaxConnectionsPerAccount != 3 || config.SubscriberBuffer != 7 ||
		config.HeartbeatInterval != 2*time.Second || config.WriteTimeout != 3*time.Second || config.MaxLifetime != 20*time.Minute {
		t.Fatalf("unexpected config: %+v", config)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	t.Setenv("SSE_MAX_CONNECTIONS", "2")
	t.Setenv("SSE_MAX_CONNECTIONS_PER_ACCOUNT", "3")
	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "cannot exceed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
