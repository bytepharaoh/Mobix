package repository

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bytepharaoh/Mobix/internal/trip/domain"
)

// setupTestDB spins up a real MongoDB container for tests.
// It returns the repository and a cleanup function to call when done.
func setupTestDB(t *testing.T) (Repository, func()) {
	t.Helper()

	ctx := context.Background()

	// start a real MongoDB container using Docker
	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	// get the connection string from the container
	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	// connect to it
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect to mongodb: %v", err)
	}

	// ping to confirm connection
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("failed to ping mongodb: %v", err)
	}

	database := client.Database("mobix_test")
	repo := NewMongoRepository(database)

	// cleanup function — called at end of each test
	cleanup := func() {
		if err := client.Disconnect(ctx); err != nil {
			t.Logf("failed to disconnect mongo: %v", err)
		}
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	return repo, cleanup
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		trip    *domain.Trip
		wantErr bool
	}{
		{
			name: "successfully creates a trip",
			trip: &domain.Trip{
				RiderID: "rider123",
				Pickup:  domain.Location{Lat: 30.0, Lng: 31.0},
				Dropoff: domain.Location{Lat: 30.1, Lng: 31.1},
				Status:  domain.StatusRequested,
			},
			wantErr: false,
		},
		{
			name: "creates trip with empty driver id",
			trip: &domain.Trip{
				RiderID:  "rider456",
				DriverID: "",
				Pickup:   domain.Location{Lat: 30.0, Lng: 31.0},
				Dropoff:  domain.Location{Lat: 30.1, Lng: 31.1},
				Status:   domain.StatusRequested,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, cleanup := setupTestDB(t)
			defer cleanup()

			result, err := repo.Create(context.Background(), tt.trip)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// ID must be set after insert
			if result.ID == bson.NilObjectID {
				t.Error("expected ID to be set after create")
			}

			// timestamps must be set
			if result.CreatedAt.IsZero() {
				t.Error("expected CreatedAt to be set")
			}
			if result.UpdatedAt.IsZero() {
				t.Error("expected UpdatedAt to be set")
			}

			// data must match what we sent
			if result.RiderID != tt.trip.RiderID {
				t.Errorf("got rider_id %s, want %s", result.RiderID, tt.trip.RiderID)
			}
		})
	}
}

func TestFindByID(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo Repository) string // returns the ID to search for
		wantErr bool
	}{
		{
			name: "finds existing trip",
			setup: func(repo Repository) string {
				trip := &domain.Trip{
					RiderID: "rider123",
					Pickup:  domain.Location{Lat: 30.0, Lng: 31.0},
					Dropoff: domain.Location{Lat: 30.1, Lng: 31.1},
					Status:  domain.StatusRequested,
				}
				created, err := repo.Create(context.Background(), trip)
				if err != nil {
					return ""
				}
				return created.ID.Hex()
			},
			wantErr: false,
		},
		{
			name: "returns error for non-existent trip",
			setup: func(repo Repository) string {
				return bson.NewObjectID().Hex() // valid format but doesn't exist
			},
			wantErr: true,
		},
		{
			name: "returns error for invalid id format",
			setup: func(repo Repository) string {
				return "not-a-valid-objectid"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, cleanup := setupTestDB(t)
			defer cleanup()

			id := tt.setup(repo)
			result, err := repo.FindByID(context.Background(), id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("expected trip but got nil")
				return
			}

			if result.ID.Hex() != id {
				t.Errorf("got id %s, want %s", result.ID.Hex(), id)
			}
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name      string
		newStatus domain.TripStatus
		setup     func(repo Repository) string
		wantErr   bool
	}{
		{
			name:      "successfully updates status to assigned",
			newStatus: domain.StatusAssigned,
			setup: func(repo Repository) string {
				trip := &domain.Trip{
					RiderID: "rider123",
					Status:  domain.StatusRequested,
					Pickup:  domain.Location{Lat: 30.0, Lng: 31.0},
					Dropoff: domain.Location{Lat: 30.1, Lng: 31.1},
				}
				created, _ := repo.Create(context.Background(), trip)
				return created.ID.Hex()
			},
			wantErr: false,
		},
		{
			name:      "returns error for non-existent trip",
			newStatus: domain.StatusAssigned,
			setup: func(repo Repository) string {
				return bson.NewObjectID().Hex()
			},
			wantErr: true,
		},
		{
			name:      "returns error for invalid id",
			newStatus: domain.StatusAssigned,
			setup: func(repo Repository) string {
				return "bad-id"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, cleanup := setupTestDB(t)
			defer cleanup()

			id := tt.setup(repo)
			err := repo.UpdateStatus(context.Background(), id, tt.newStatus)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// verify the status actually changed in MongoDB
			updated, err := repo.FindByID(context.Background(), id)
			if err != nil {
				t.Errorf("failed to fetch updated trip: %v", err)
				return
			}

			if updated.Status != tt.newStatus {
				t.Errorf("got status %s, want %s", updated.Status, tt.newStatus)
			}

			// verify UpdatedAt was refreshed
			if updated.UpdatedAt.Before(time.Now().Add(-5 * time.Second)) {
				t.Error("expected UpdatedAt to be recent")
			}
		})
	}
}
