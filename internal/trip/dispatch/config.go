package dispatch

import (
	"fmt"
	"strconv"
	"time"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/pkg/config"
)

type Config struct {
	BatchSize      int
	RadiusMeters   int
	LocationMaxAge time.Duration
	OfferTTL       time.Duration
}

func LoadConfig() (Config, error) {
	batch, err := strconv.Atoi(config.GetString("DISPATCH_BATCH_SIZE", "3"))
	if err != nil || batch < 1 || batch > 10 {
		return Config{}, fmt.Errorf("DISPATCH_BATCH_SIZE must be between 1 and 10")
	}
	radius, err := strconv.Atoi(config.GetString("DISPATCH_RADIUS_METERS", "8000"))
	if err != nil || radius < 1 || radius > driver.MaxDispatchRadiusMeters {
		return Config{}, fmt.Errorf("DISPATCH_RADIUS_METERS must be between 1 and %d", driver.MaxDispatchRadiusMeters)
	}
	age, err := time.ParseDuration(config.GetString("DISPATCH_LOCATION_MAX_AGE", "90s"))
	if err != nil || age < time.Second || age > 10*time.Minute {
		return Config{}, fmt.Errorf("DISPATCH_LOCATION_MAX_AGE must be between 1s and 10m")
	}
	ttl, err := time.ParseDuration(config.GetString("DISPATCH_OFFER_TTL", "15s"))
	if err != nil || ttl < time.Second || ttl > time.Minute {
		return Config{}, fmt.Errorf("DISPATCH_OFFER_TTL must be between 1s and 1m")
	}
	return Config{BatchSize: batch, RadiusMeters: radius, LocationMaxAge: age, OfferTTL: ttl}, nil
}
