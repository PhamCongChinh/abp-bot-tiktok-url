package repository

import (
	"context"
	"fmt"
	"time"

	"abp-bot-tiktok-url/internal/models"
	"abp-bot-tiktok-url/internal/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// VideoStore abstracts video persistence operations.
// Define at consumer side — Crawler depends on this, not on *VideoRepository.
type VideoStore interface {
	FindBySourceURL(ctx context.Context, sourceURL string, limit int64) ([]VideoDocument, error)
	UpsertVideo(ctx context.Context, video models.VideoItem) error
	BulkUpsert(ctx context.Context, videos []models.VideoItem) error
}

type VideoDocument struct {
	SourceURL   string    `bson:"source_url"`
	VideoID     string    `bson:"video_id"`
	Description string    `bson:"description"`
	PubTime     int64     `bson:"pub_time"`
	UniqueID    string    `bson:"unique_id"`
	AuthID      string    `bson:"auth_id"`
	AuthName    string    `bson:"auth_name"`
	Comments    int64     `bson:"comments"`
	Shares      int64     `bson:"shares"`
	Reactions   int64     `bson:"reactions"`
	Favors      int64     `bson:"favors"`
	Views       int64     `bson:"views"`
	CrawledAt   time.Time `bson:"crawled_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

type VideoRepository struct {
	collection *mongo.Collection
	log        *zap.Logger
}

func NewVideoRepository(db *mongo.Database, log *zap.Logger) *VideoRepository {
	collection := db.Collection("tiktok_videos")

	// Tạo index cho video_id (unique) để tránh duplicate
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "video_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}

	_, err := collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		log.Warn("Failed to create index on video_id (may already exist)", zap.Error(err))
	} else {
		log.Info("Index created on tiktok_videos.video_id")
	}

	return &VideoRepository{
		collection: collection,
		log:        log,
	}
}

func (r *VideoRepository) Upsert(ctx context.Context, video models.VideoItem) error {
	return r.UpsertVideo(ctx, video)
}

// UpsertVideo inserts or updates a single video document. Satisfies the
// VideoStore interface.
func (r *VideoRepository) UpsertVideo(ctx context.Context, video models.VideoItem) error {
	now := time.Now()
	doc := VideoDocument{
		SourceURL:   video.SourceURL,
		VideoID:     video.VideoID,
		Description: video.Description,
		PubTime:     video.PubTime,
		UniqueID:    video.UniqueID,
		AuthID:      video.AuthID,
		AuthName:    video.AuthName,
		Comments:    video.Comments,
		Shares:      video.Shares,
		Reactions:   video.Reactions,
		Favors:      video.Favors,
		Views:       video.Views,
		CrawledAt:   now,
		UpdatedAt:   now,
	}

	filter := bson.M{"video_id": video.VideoID}
	update := bson.M{
		"$set": doc,
		"$setOnInsert": bson.M{
			"crawled_at": now,
		},
	}
	opts := options.Update().SetUpsert(true)

	err := utils.RetryWithBackoff(ctx, 3, func() error {
		_, err := r.collection.UpdateOne(ctx, filter, update, opts)
		return err
	})
	if err != nil {
		return fmt.Errorf("Upsert video %s: %w", video.VideoID, err)
	}
	return nil
}

func (r *VideoRepository) BulkUpsert(ctx context.Context, videos []models.VideoItem) error {
	if len(videos) == 0 {
		return nil
	}

	now := time.Now()
	var operations []mongo.WriteModel

	for _, video := range videos {
		doc := VideoDocument{
			SourceURL:   video.SourceURL,
			VideoID:     video.VideoID,
			Description: video.Description,
			PubTime:     video.PubTime,
			UniqueID:    video.UniqueID,
			AuthID:      video.AuthID,
			AuthName:    video.AuthName,
			Comments:    video.Comments,
			Shares:      video.Shares,
			Reactions:   video.Reactions,
			Favors:      video.Favors,
			Views:       video.Views,
			CrawledAt:   now,
			UpdatedAt:   now,
		}

		filter := bson.M{"video_id": video.VideoID}
		update := bson.M{
			"$set": doc,
			"$setOnInsert": bson.M{
				"crawled_at": now,
			},
		}

		operation := mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true)

		operations = append(operations, operation)
	}

	opts := options.BulkWrite().SetOrdered(false)

	var result *mongo.BulkWriteResult
	err := utils.RetryWithBackoff(ctx, 3, func() error {
		var bulkErr error
		result, bulkErr = r.collection.BulkWrite(ctx, operations, opts)
		return bulkErr
	})
	if err != nil {
		return fmt.Errorf("BulkUpsert %d videos: %w", len(videos), err)
	}

	r.log.Info("Bulk upsert completed",
		zap.Int64("inserted", result.InsertedCount),
		zap.Int64("modified", result.ModifiedCount),
		zap.Int64("upserted", result.UpsertedCount),
	)

	return nil
}

func (r *VideoRepository) FindBySourceURL(ctx context.Context, sourceURL string, limit int64) ([]VideoDocument, error) {
	filter := bson.M{"source_url": sourceURL}
	opts := options.Find().
		SetSort(bson.D{{Key: "pub_time", Value: -1}}).
		SetLimit(limit)

	var cursor *mongo.Cursor
	err := utils.RetryWithBackoff(ctx, 3, func() error {
		var findErr error
		cursor, findErr = r.collection.Find(ctx, filter, opts)
		return findErr
	})
	if err != nil {
		return nil, fmt.Errorf("FindBySourceURL %q: %w", sourceURL, err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var videos []VideoDocument
	if err := cursor.All(ctx, &videos); err != nil {
		return nil, fmt.Errorf("FindBySourceURL %q cursor.All: %w", sourceURL, err)
	}

	return videos, nil
}
