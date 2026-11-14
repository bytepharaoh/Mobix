package realtime

import (
	"errors"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/messaging"
)

func TestHubLimitsDeduplicatesAndFilters(t *testing.T) {
	config := DefaultConfig()
	config.MaxConnections = 2
	config.MaxConnectionsPerAccount = 1
	config.SubscriberBuffer = 2
	hub := NewHub(config)
	subscription, err := hub.Subscribe("rider:rider-a", func(event messaging.RideEvent) bool {
		return event.TripID == "trip-a"
	})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if _, err := hub.Subscribe("rider:rider-a", nil); !errors.Is(err, ErrConnectionLimit) {
		t.Fatalf("second account connection error = %v", err)
	}

	event := validEvent("trip-a", 1)
	hub.Publish(validEvent("trip-b", 1))
	hub.Publish(event)
	hub.Publish(event)
	select {
	case notification := <-subscription.Events:
		if notification.Event.ID != event.ID {
			t.Fatalf("received event %q, want %q", notification.Event.ID, event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("matching event was not delivered")
	}
	select {
	case extra := <-subscription.Events:
		t.Fatalf("duplicate or non-matching event was delivered: %+v", extra)
	default:
	}

	subscription.Close()
	replacement, err := hub.Subscribe("rider:rider-a", nil)
	if err != nil {
		t.Fatalf("closing did not release account capacity: %v", err)
	}
	replacement.Close()
}

func TestHubDisconnectsSlowSubscribersAndBroadcastsResync(t *testing.T) {
	config := DefaultConfig()
	config.MaxConnections = 2
	config.MaxConnectionsPerAccount = 2
	config.SubscriberBuffer = 1
	hub := NewHub(config)
	slow, err := hub.Subscribe("driver:driver-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	hub.Publish(validEvent("trip-a", 1))
	hub.Publish(validEvent("trip-a", 2))
	select {
	case <-slow.Done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber was not disconnected")
	}

	current, err := hub.Subscribe("driver:driver-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	hub.Resync()
	select {
	case notification := <-current.Events:
		if !notification.Resync {
			t.Fatalf("expected resync notification, got %+v", notification)
		}
	case <-time.After(time.Second):
		t.Fatal("resync was not delivered")
	}
}

func validEvent(tripID string, version int64) messaging.RideEvent {
	return messaging.RideEvent{
		ID: "ride-event:" + tripID + ":" + stringVersion(version), TripID: tripID,
		Type: messaging.RideTripStatusChanged, Status: "requested", Version: version, OccurredAt: time.Now().UTC(),
	}
}

func stringVersion(version int64) string {
	if version == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for version > 0 {
		index--
		digits[index] = byte('0' + version%10)
		version /= 10
	}
	return string(digits[index:])
}
