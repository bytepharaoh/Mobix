package mongox

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/config"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func Open(ctx context.Context) (*mongo.Database, error) {
	uri := config.GetString("MONGO_URI", "")
	if uri == "" {
		address := url.URL{
			Scheme:   "mongodb",
			User:     url.UserPassword(config.GetString("MONGO_USER", "admin"), config.GetString("MONGO_PASSWORD", "password")),
			Host:     net.JoinHostPort(config.GetString("MONGO_HOST", "localhost"), config.GetString("MONGO_PORT", "27017")),
			Path:     "/",
			RawQuery: "authSource=admin",
		}
		uri = address.String()
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(5 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("configure MongoDB: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		Close(client)
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}
	return client.Database(config.GetString("MONGO_DB", "mobix")), nil
}

func Close(client *mongo.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		slog.Error("close MongoDB", "error", err)
	}
}
