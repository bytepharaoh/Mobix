package repository

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bytepharaoh/Mobix/internal/driver/domain"
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

	// cleanup function called at end of each test
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

func createTestDriver(t *testing.T, repo Repository) *domain.Driver {
	t.Helper()

	driver, err := repo.Create(context.Background(), &domain.Driver{
		Name:      "Pavel Khramov",
		Email:     "pashka@example.com",
		Phone:     "0123456789",
		CarNumber: "SAM777",
		CarModel:  "Bugatti veiron",
		Location: domain.Location{
			Lat: 67.67,
			Lng: 67.67,
		},
		Status: domain.StatusAvailable,
	})

	if err != nil {
		t.Fatalf("failed to create driver: %v", err)
	}

	return driver
}

func TestCreateDriver(t *testing.T) {
	repo, _ := setupTestDB(t)
	driver := createTestDriver(t, repo)
	driverWithEmptyEmail := createTestDriver(t, repo)
	driverWithEmptyEmail.Email = ""

	tests := []struct {
		name    string
		driver  *domain.Driver
		wantErr bool
	}{
		{
			name:    "successfully creates a driver",
			driver:  driver,
			wantErr: false,
		},
		{
			name:    "creates driver with empty email",
			driver:  driverWithEmptyEmail,
			wantErr: false, // repository doesn't validate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, cleanup := setupTestDB(t)
			defer cleanup()

			result, err := repo.Create(context.Background(), tt.driver)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// ID must be generated
			if result.ID == bson.NilObjectID {
				t.Error("expected ID to be set")
			}

			// timestamps must be set
			if result.CreatedAt.IsZero() {
				t.Error("expected CreatedAt to be set")
			}

			if result.UpdatedAt.IsZero() {
				t.Error("expected UpdatedAt to be set")
			}

			// verify fields
			if result.Name != tt.driver.Name {
				t.Errorf("got name %q, want %q", result.Name, tt.driver.Name)
			}

			if result.Email != tt.driver.Email {
				t.Errorf("got email %q, want %q", result.Email, tt.driver.Email)
			}

			if result.Phone != tt.driver.Phone {
				t.Errorf("got phone %q, want %q", result.Phone, tt.driver.Phone)
			}

			if result.CarNumber != tt.driver.CarNumber {
				t.Errorf("got car number %q, want %q", result.CarNumber, tt.driver.CarNumber)
			}

			if result.CarModel != tt.driver.CarModel {
				t.Errorf("got car model %q, want %q", result.CarModel, tt.driver.CarModel)
			}

			if result.Status != tt.driver.Status {
				t.Errorf("got status %q, want %q", result.Status, tt.driver.Status)
			}
		})
	}
}

func TestFindByID(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	tests := []struct {
		name    string
		setup   func(repo Repository) string
		wantErr bool
	}{
		{
			name: "finds existing driver",
			setup: func(repo Repository) string {
				driver := createTestDriver(t, repo)

				return driver.ID.Hex()
			},
			wantErr: false,
		},
		{
			name: "returns error for non-existent driver",
			setup: func(repo Repository) string {
				return bson.NewObjectID().Hex()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.setup(repo)

			result, err := repo.FindById(context.Background(), id)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("expected driver but got nil")
			}

			if result.ID.Hex() != id {
				t.Errorf("got id %s, want %s", result.ID.Hex(), id)
			}

			if result.Name != "John Doe" {
				t.Errorf("got name %s, want John Doe", result.Name)
			}

			if result.Email != "john@example.com" {
				t.Errorf("got email %s, want john@example.com", result.Email)
			}

			if result.Status != domain.StatusAvailable {
				t.Errorf("got status %s, want %s",
					result.Status,
					domain.StatusAvailable,
				)
			}
		})
	}
}

func TestIncreaseTotalTrips(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	driver := createTestDriver(t, repo)

	err := repo.IncreaseTotalTrips(
		context.Background(),
		driver.ID.Hex(),
		5,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := repo.FindById(
		context.Background(),
		driver.ID.Hex(),
	)
	if err != nil {
		t.Fatalf("failed to fetch driver: %v", err)
	}

	if updated.TotalTrips < 5 {
		t.Errorf("total trips %d, less than delta we increased on", updated.TotalTrips)
	}
}

func TestUpdateStatus(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	driver := createTestDriver(t, repo)

	err := repo.UpdateStatus(
		context.Background(),
		driver.ID.Hex(),
		domain.StatusOnTrip,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := repo.FindById(
		context.Background(),
		driver.ID.Hex(),
	)
	if err != nil {
		t.Fatalf("failed to fetch driver: %v", err)
	}

	if updated.Status != domain.StatusOnTrip {
		t.Errorf(
			"got status %s, want %s",
			updated.Status,
			domain.StatusOnTrip,
		)
	}
}

func TestUpdateLastLogin(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	driver := createTestDriver(t, repo)

	loginTime := time.Now().UTC().Truncate(time.Second)

	err := repo.UpdateLastLogin(
		context.Background(),
		driver.ID.Hex(),
		loginTime,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := repo.FindById(
		context.Background(),
		driver.ID.Hex(),
	)
	if err != nil {
		t.Fatalf("failed to fetch driver: %v", err)
	}

	if !updated.LastLoginAt.Equal(loginTime) {
		t.Errorf(
			"got login time %v, want %v",
			updated.LastLoginAt,
			loginTime,
		)
	}
}

func TestUpdateRating(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	driver := createTestDriver(t, repo)

	err := repo.UpdateRating(
		context.Background(),
		driver.ID.Hex(),
		4.8,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := repo.FindById(
		context.Background(),
		driver.ID.Hex(),
	)
	if err != nil {
		t.Fatalf("failed to fetch driver: %v", err)
	}

	if updated.Rating != 4.8 {
		t.Errorf("got rating %f, want 4.8", updated.Rating)
	}
}
