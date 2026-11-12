package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRidePublisherConfirmsRoutingBeforeDrainingOutbox(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "rabbitmq:4.2-management",
		testcontainers.WithEnv(map[string]string{
			"RABBITMQ_DEFAULT_USER": "mobix",
			"RABBITMQ_DEFAULT_PASS": "mobix-test",
		}),
		testcontainers.WithExposedPorts("5672/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5672/tcp").WithStartupTimeout(45*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "5672/tcp")
	if err != nil {
		t.Fatal(err)
	}
	uri := fmt.Sprintf("amqp://mobix:mobix-test@%s:%s/", host, port.Port())
	event := RideEvent{
		ID: "ride-event:trip-a:7", TripID: "trip-a", Type: RideOffersUpdated,
		Status: "requested", RiderAccountID: "rider-a", OfferAccountIDs: []string{"driver-a"},
		Version: 7, OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	outbox := &rideOutboxStub{event: &event, published: make(chan RideEvent, 1)}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go RunRidePublisher(workerCtx, uri, outbox)

	select {
	case published := <-outbox.published:
		if published.ID != event.ID {
			t.Fatalf("published event ID = %q, want %q", published.ID, event.ID)
		}
	case <-ctx.Done():
		t.Fatal("publisher did not confirm and drain the event before the deadline")
	}

	connection, err := amqp.Dial(uri)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	delivery, ok, err := channel.Get(RideBufferQueue, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("confirmed event was not routed to the ride-event queue")
	}
	defer delivery.Ack(false)
	if delivery.Type != RideEventKind || delivery.MessageId != event.ID || delivery.RoutingKey != string(event.Type) {
		t.Fatalf("incorrect AMQP metadata: type=%q id=%q key=%q", delivery.Type, delivery.MessageId, delivery.RoutingKey)
	}
	var received RideEvent
	if err := json.Unmarshal(delivery.Body, &received); err != nil {
		t.Fatal(err)
	}
	if received.ID != event.ID || received.Version != event.Version || received.TripID != event.TripID {
		t.Fatalf("incorrect published event: %+v", received)
	}
}

type rideOutboxStub struct {
	mu        sync.Mutex
	event     *RideEvent
	published chan RideEvent
}

func (outbox *rideOutboxStub) NextRideEvent(context.Context) (*RideEvent, error) {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if outbox.event == nil {
		return nil, nil
	}
	event := *outbox.event
	return &event, nil
}

func (outbox *rideOutboxStub) MarkRideEventPublished(_ context.Context, event RideEvent) error {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if outbox.event == nil || outbox.event.ID != event.ID {
		return fmt.Errorf("unexpected published event %q", event.ID)
	}
	outbox.event = nil
	outbox.published <- event
	return nil
}
