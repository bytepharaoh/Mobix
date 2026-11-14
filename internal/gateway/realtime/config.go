package realtime

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	MaxConnections           int
	MaxConnectionsPerAccount int
	SubscriberBuffer         int
	HeartbeatInterval        time.Duration
	WriteTimeout             time.Duration
	MaxLifetime              time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxConnections: 1_000, MaxConnectionsPerAccount: 5, SubscriberBuffer: 32,
		HeartbeatInterval: 15 * time.Second, WriteTimeout: 5 * time.Second, MaxLifetime: 15 * time.Minute,
	}
}

func LoadConfig() (Config, error) {
	config := DefaultConfig()
	var err error
	if config.MaxConnections, err = environmentInt("SSE_MAX_CONNECTIONS", config.MaxConnections, 1, 100_000); err != nil {
		return Config{}, err
	}
	if config.MaxConnectionsPerAccount, err = environmentInt("SSE_MAX_CONNECTIONS_PER_ACCOUNT", config.MaxConnectionsPerAccount, 1, 100); err != nil {
		return Config{}, err
	}
	if config.SubscriberBuffer, err = environmentInt("SSE_SUBSCRIBER_BUFFER", config.SubscriberBuffer, 1, 1_000); err != nil {
		return Config{}, err
	}
	if config.HeartbeatInterval, err = environmentDuration("SSE_HEARTBEAT_INTERVAL", config.HeartbeatInterval, time.Second, time.Minute); err != nil {
		return Config{}, err
	}
	if config.WriteTimeout, err = environmentDuration("SSE_WRITE_TIMEOUT", config.WriteTimeout, time.Second, 30*time.Second); err != nil {
		return Config{}, err
	}
	if config.MaxLifetime, err = environmentDuration("SSE_MAX_LIFETIME", config.MaxLifetime, time.Minute, 24*time.Hour); err != nil {
		return Config{}, err
	}
	if config.MaxConnectionsPerAccount > config.MaxConnections {
		return Config{}, fmt.Errorf("SSE_MAX_CONNECTIONS_PER_ACCOUNT cannot exceed SSE_MAX_CONNECTIONS")
	}
	return config, nil
}

func environmentInt(name string, fallback, minimum, maximum int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func environmentDuration(name string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return parsed, nil
}
