package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// URLStore abstracts URL persistence operations.
// Define at consumer side so main() and Crawler depend on this, not on
// *URLRepository.
type URLStore interface {
	FindByOrgIDs(orgIDs []int) ([]URLEntry, error)
}

// URLEntry is a single crawl source document from `source_crawl`, scoped to
// one or more organisations (projectId). Only documents with
// source="tiktok" are relevant to this bot.
type URLEntry struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	ProjectID       []int              `bson:"projectId"`
	SourceType      string             `bson:"source_type"`
	Source          string             `bson:"source"`
	Name            string             `bson:"name"`
	URL             string             `bson:"url"`
	SourceOwnership string             `bson:"source_ownership"`
}

type URLRepository struct {
	collection *mongo.Collection
	log        *zap.Logger
}

func NewURLRepository(db *mongo.Database, log *zap.Logger) *URLRepository {
	return &URLRepository{
		collection: db.Collection("source_crawl"),
		log:        log,
	}
}

// FindByOrgIDs returns every `source_crawl` document whose projectId array
// contains any of orgIDs and whose source is "tiktok".
func (r *URLRepository) FindByOrgIDs(orgIDs []int) ([]URLEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"projectId": bson.M{"$in": orgIDs},
		"source":    "tiktok",
	}
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var urls []URLEntry
	if err := cursor.All(ctx, &urls); err != nil {
		return nil, err
	}

	return urls, nil
}
