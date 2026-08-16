package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// OrgStore abstracts organisation lookup operations.
// Define at consumer side so Crawler depends on this, not on *OrgRepository.
type OrgStore interface {
	FindActiveOrgIDs() ([]int, error)
}

// orgDoc is the subset of the `org` collection schema needed to resolve
// active org_ids.
type orgDoc struct {
	OrgID int `bson:"org_id"`
}

type OrgRepository struct {
	collection *mongo.Collection
	log        *zap.Logger
}

func NewOrgRepository(db *mongo.Database, log *zap.Logger) *OrgRepository {
	return &OrgRepository{
		collection: db.Collection("org"),
		log:        log,
	}
}

// FindActiveOrgIDs returns the org_id of every org document with
// status="ACTIVE".
func (r *OrgRepository) FindActiveOrgIDs() ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"status": "ACTIVE"}
	opts := options.Find().SetProjection(bson.M{"org_id": 1})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var docs []orgDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	orgIDs := make([]int, 0, len(docs))
	for _, d := range docs {
		orgIDs = append(orgIDs, d.OrgID)
	}
	return orgIDs, nil
}
