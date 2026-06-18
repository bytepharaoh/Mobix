package db

import (
	"context"
	"fmt"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/config"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// DB holds the active MongoDB client and the target database.
// Repositories use db.Database.Collection("name") to access collections.
type DB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// Connect builds the connection URI from env vars, connects, pings, and returns a ready DB.
func Connect() (*DB, error) {
	mongoUser := config.GetString("MONGO_USER", "admin")
	mongoPwd  := config.GetString("MONGO_PASSWORD", "password")
	mongoHost := config.GetString("MONGO_HOST", "localhost")
	mongoPort := config.GetString("MONGO_PORT", "27017")
	mongoName := config.GetString("MONGO_DB", "mobix")

	uri := fmt.Sprintf(
		"mongodb://%s:%s@%s:%s",
		mongoUser, mongoPwd, mongoHost, mongoPort,
	)

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	return &DB{
		Client:   client,
		Database: client.Database(mongoName),
	}, nil
	}

	// Disconnect closes the MongoDB connection cleanly.
func Disconnect(database *DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := database.Client.Disconnect(ctx); err != nil {
		return fmt.Errorf("mongo disconnect: %w", err)
	}
	return nil
}
