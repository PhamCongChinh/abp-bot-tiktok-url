package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.uber.org/zap"
)

// Database abstracts MongoDB database access.
// Define at provider side so consumers can accept this interface for testability.
type Database interface {
	Collection(name string) *mongo.Collection
}

type MongoDB struct {
	client *mongo.Client
	db     *mongo.Database
	log    *zap.Logger
}

func NewMongoDB(ctx context.Context, uri, dbName string, maxPoolSize, minPoolSize uint64, log *zap.Logger) (*MongoDB, error) {
	clientOpts := options.Client().
		ApplyURI(uri).
		SetMaxPoolSize(maxPoolSize).
		SetMinPoolSize(minPoolSize).
		SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongodb connect: %w", err)
	}

	// Health check — ping the primary node.
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("mongodb ping: %w", err)
	}

	log.Info("MongoDB connected",
		zap.String("database", dbName),
		zap.Uint64("maxPoolSize", maxPoolSize),
		zap.Uint64("minPoolSize", minPoolSize),
	)

	return &MongoDB{
		client: client,
		db:     client.Database(dbName),
		log:    log,
	}, nil
}

func (m *MongoDB) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.client.Disconnect(ctx)
}

func (m *MongoDB) Database() *mongo.Database {
	return m.db
}

func (m *MongoDB) Collection(name string) *mongo.Collection {
	return m.db.Collection(name)
}
