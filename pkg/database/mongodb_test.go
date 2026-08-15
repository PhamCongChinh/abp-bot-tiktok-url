package database

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
	"go.uber.org/zap"
)

func TestNewMongoDB_InvalidURI(t *testing.T) {
	log := zap.NewNop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := NewMongoDB(ctx, "not-a-valid-mongo-uri", "testdb", 100, 10, log)
	if err == nil {
		t.Error("expected error for invalid URI, got nil")
	}
}

func TestNewMongoDB_ContextCancelled(t *testing.T) {
	log := zap.NewNop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewMongoDB(ctx, "mongodb://localhost:27017", "testdb", 100, 10, log)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestNewMongoDB_ZeroPoolSizes(t *testing.T) {
	log := zap.NewNop()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := NewMongoDB(ctx, "mongodb://localhost:27017", "testdb", 0, 0, log)
	if err == nil {
		t.Log("unexpected success (server may be running)")
	}
}

// Test MongoDB struct accessor methods using mtest mock client.
func TestMongoDB_Accessors(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("database and collection accessors", func(mt *mtest.T) {
		db := &MongoDB{
			client: mt.Client,
			db:     mt.DB,
			log:    zap.NewNop(),
		}

		// Test Database().
		d := db.Database()
		if d == nil {
			mt.Fatal("Database() returned nil")
		}
		if d.Name() != mt.DB.Name() {
			mt.Errorf("Database name = %q, want %q", d.Name(), mt.DB.Name())
		}

		// Test Collection().
		coll := db.Collection("test_coll")
		if coll == nil {
			mt.Fatal("Collection() returned nil")
		}
		if coll.Name() != "test_coll" {
			mt.Errorf("Collection name = %q, want %q", coll.Name(), "test_coll")
		}
	})
}
