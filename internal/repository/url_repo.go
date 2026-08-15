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
	FindActive() ([]URLEntry, error)
}

// URLEntry is a single TikTok URL (video or profile) to crawl directly,
// scoped to an organisation.
type URLEntry struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	URL    string             `bson:"url"`
	OrgID  int                `bson:"org_id"`
	Active bool               `bson:"active"`
}

type URLRepository struct {
	collection *mongo.Collection
	log        *zap.Logger
}

func NewURLRepository(db *mongo.Database, log *zap.Logger) *URLRepository {
	return &URLRepository{
		collection: db.Collection("tiktok_url"),
		log:        log,
	}
}

func (r *URLRepository) FindByOrgIDs(orgIDs []int) ([]URLEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"org_id": bson.M{"$in": orgIDs}}
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

func (r *URLRepository) FindActive() ([]URLEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"active": true}
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
