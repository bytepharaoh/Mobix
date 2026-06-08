package db

import (
	"context"
	"fmt"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/config"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func ConnectMongo() (*mongo.Client, error) {
	// Getting .env variables
	mongoUser := config.GetString("MONGO_USER", "admin")
	mongoPwd := config.GetString("MONGO_PASSWORD", "password")
	//mongoHost := config.GetString("MONGO_HOST", "mongodb")
	// For development process
	mongoHost := config.GetString("MONGO_HOST_OUTSIDE_DOCKER", "localhost")
	mongoPort := config.GetString("MONGO_PORT", "27017")

	// Constructing Mongo connection uri
	mongoUri := fmt.Sprintf(
		"mongodb://%s:%s@%s:%s",
		mongoUser,
		mongoPwd,
		mongoHost,
		mongoPort,
	)

	client, err := mongo.Connect(options.Client().ApplyURI(mongoUri))

	if err != nil {
		return nil, fmt.Errorf("mongo connection. %w", err)
	}

	// Creating a context to ping mongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// To cancel context in any case
	defer cancel()

	// Ping mongoDB
	if err := client.Ping(ctx, nil); err != nil {
		// Disconnect and return error if smth goes wrong
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongo. %w", err)
	}

	return client, nil
}

func DisconnectMongo(client *mongo.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect mongo: %w", err)
	}

	return nil
}