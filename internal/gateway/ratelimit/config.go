package ratelimit

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Operation string

const (
	LoginIP       Operation = "login_ip"
	LoginAccount  Operation = "login_account"
	RideRequest   Operation = "ride_request"
	Location      Operation = "location_update"
	OfferPoll     Operation = "offer_poll"
	OfferResponse Operation = "offer_response"
)

type Policy struct {
	Capacity int64
	Window   time.Duration
}

type Config struct {
	RedisURL         string
	KeySecret        string
	TrustedProxyCIDR string
	Policies         map[Operation]Policy
}

func LoadConfig() (Config, error) {
	config := Config{
		RedisURL:         os.Getenv("REDIS_URL"),
		KeySecret:        os.Getenv("RATE_LIMIT_KEY_SECRET"),
		TrustedProxyCIDR: os.Getenv("TRUSTED_PROXY_CIDRS"),
		Policies:         make(map[Operation]Policy, 6),
	}
	if config.RedisURL == "" {
		return Config{}, fmt.Errorf("REDIS_URL is required")
	}
	if len(config.KeySecret) < 32 {
		return Config{}, fmt.Errorf("RATE_LIMIT_KEY_SECRET must be at least 32 bytes")
	}
	defaults := []struct {
		operation   Operation
		environment string
		value       string
	}{
		{LoginIP, "RATE_LIMIT_LOGIN_IP", "10/1m"},
		{LoginAccount, "RATE_LIMIT_LOGIN_ACCOUNT", "5/15m"},
		{RideRequest, "RATE_LIMIT_RIDE_REQUEST", "20/1m"},
		{Location, "RATE_LIMIT_LOCATION_UPDATE", "120/1m"},
		{OfferPoll, "RATE_LIMIT_OFFER_POLL", "120/1m"},
		{OfferResponse, "RATE_LIMIT_OFFER_RESPONSE", "30/1m"},
	}
	for _, item := range defaults {
		value := os.Getenv(item.environment)
		if value == "" {
			value = item.value
		}
		policy, err := parsePolicy(value)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", item.environment, err)
		}
		config.Policies[item.operation] = policy
	}
	return config, nil
}

func parsePolicy(value string) (Policy, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return Policy{}, fmt.Errorf("use requests/duration, for example 10/1m")
	}
	capacity, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || capacity < 1 || capacity > 1_000_000 {
		return Policy{}, fmt.Errorf("request capacity must be between 1 and 1000000")
	}
	window, err := time.ParseDuration(parts[1])
	if err != nil || window < time.Second || window > 24*time.Hour {
		return Policy{}, fmt.Errorf("duration must be between 1s and 24h")
	}
	return Policy{Capacity: capacity, Window: window}, nil
}
