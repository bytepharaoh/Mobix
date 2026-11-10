package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/messaging"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestRecordDeduplicatesConcurrentDeliveries(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	container, err := mongodb.Run(ctx, "mongo:8.0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	})
	repo := New(client.Database("test_payments"))
	id := bson.NewObjectID().Hex()
	event := messaging.TripCompleted{
		ID: "trip-completed:" + id, TripID: id, AmountCents: 750,
		Currency: "USD", OccurredAt: time.Now(),
	}
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			if err := repo.Record(ctx, event); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	receipt, err := repo.FindByTrip(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TripID != id || receipt.EventID != event.ID || receipt.AmountCents != 750 ||
		receipt.Currency != "USD" || receipt.Status != "simulated_paid" {
		t.Fatalf("incorrect receipt: %+v", receipt)
	}
	count, err := repo.collection.CountDocuments(ctx, bson.D{})
	if err != nil || count != 1 {
		t.Fatalf("expected one receipt, got %d, error: %v", count, err)
	}
	event.ID = "malformed"
	if err := repo.Record(ctx, event); !errors.Is(err, messaging.ErrInvalidEvent) {
		t.Fatalf("expected invalid event error, got %v", err)
	}
}
