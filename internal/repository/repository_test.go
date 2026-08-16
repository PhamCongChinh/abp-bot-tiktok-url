package repository

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"abp-bot-tiktok-url/internal/models"

	"github.com/pashagolub/pgxmock/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
	"go.uber.org/zap"
)

func repoTestLogger() *zap.Logger {
	return zap.NewNop()
}

func TestNewVideoRepository(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("create repository", func(mt *mtest.T) {
		db := mt.DB
		repo := NewVideoRepository(db, repoTestLogger())
		if repo == nil {
			mt.Fatal("NewVideoRepository returned nil")
		}
		if repo.collection == nil {
			mt.Fatal("collection is nil")
		}
		if repo.log == nil {
			mt.Fatal("logger is nil")
		}
	})
}

func TestNewURLRepository(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("create repository", func(mt *mtest.T) {
		db := mt.DB
		repo := NewURLRepository(db, repoTestLogger())
		if repo == nil {
			mt.Fatal("NewURLRepository returned nil")
		}
		if repo.collection == nil {
			mt.Error("collection is nil")
		}
	})
}

func TestNewBotConfigRepository(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("create repository", func(mt *mtest.T) {
		db := mt.DB
		repo := NewBotConfigRepository(db, repoTestLogger())
		if repo == nil {
			mt.Fatal("NewBotConfigRepository returned nil")
		}
		if repo.collection == nil {
			mt.Error("collection is nil")
		}
	})
}

func TestURL_FindByOrgIDs(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("find by org IDs - success", func(mt *mtest.T) {
		repo := &URLRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		expected := []URLEntry{
			{ID: primitive.NewObjectID(), URL: "https://www.tiktok.com/@user1", OrgID: 1, Active: true},
			{ID: primitive.NewObjectID(), URL: "https://www.tiktok.com/@user2/video/123", OrgID: 2, Active: true},
		}

		first := mtest.CreateCursorResponse(1, "test.tiktok_url", mtest.FirstBatch,
			mustMarshalBSON(mt, expected[0]),
			mustMarshalBSON(mt, expected[1]),
		)
		killCursors := mtest.CreateCursorResponse(0, "test.tiktok_url", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		results, err := repo.FindByOrgIDs([]int{1, 2})
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			mt.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].URL != "https://www.tiktok.com/@user1" {
			mt.Errorf("first url = %q, want %q", results[0].URL, "https://www.tiktok.com/@user1")
		}
		if results[1].URL != "https://www.tiktok.com/@user2/video/123" {
			mt.Errorf("second url = %q, want %q", results[1].URL, "https://www.tiktok.com/@user2/video/123")
		}
	})

	mt.Run("find by org IDs - empty results", func(mt *mtest.T) {
		repo := &URLRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		first := mtest.CreateCursorResponse(1, "test.tiktok_url", mtest.FirstBatch)
		killCursors := mtest.CreateCursorResponse(0, "test.tiktok_url", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		results, err := repo.FindByOrgIDs([]int{999})
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			mt.Errorf("expected 0 results, got %d", len(results))
		}
	})
}

func TestURL_FindActive(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("find active urls", func(mt *mtest.T) {
		repo := &URLRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		expected := []URLEntry{
			{ID: primitive.NewObjectID(), URL: "https://www.tiktok.com/@active1", OrgID: 10, Active: true},
		}

		first := mtest.CreateCursorResponse(1, "test.tiktok_url", mtest.FirstBatch,
			mustMarshalBSON(mt, expected[0]),
		)
		killCursors := mtest.CreateCursorResponse(0, "test.tiktok_url", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		results, err := repo.FindActive()
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			mt.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Active != true {
			mt.Errorf("expected Active=true, got %v", results[0].Active)
		}
	})
}

func TestBotConfig_FindByBotName(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("find by bot name - found", func(mt *mtest.T) {
		repo := &BotConfigRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		expected := BotConfig{
			ID:      primitive.NewObjectID(),
			BotName: "testbot",
			BotType: "video",
			Active:  true,
		}

		// FindOne uses a cursor response with a single document.
		doc := mustMarshalBSON(mt, expected)
		first := mtest.CreateCursorResponse(1, "test.botconfig", mtest.FirstBatch, doc)
		killCursors := mtest.CreateCursorResponse(0, "test.botconfig", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		result, err := repo.FindByBotName("testbot")
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if result.BotName != "testbot" {
			mt.Errorf("BotName = %q, want %q", result.BotName, "testbot")
		}
		if result.BotType != "video" {
			mt.Errorf("BotType = %q, want %q", result.BotType, "video")
		}
		if !result.Active {
			mt.Error("expected Active=true")
		}
	})

	mt.Run("find by bot name - not found", func(mt *mtest.T) {
		repo := &BotConfigRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		// Empty cursor response simulates no matching documents.
		first := mtest.CreateCursorResponse(1, "test.botconfig", mtest.FirstBatch)
		killCursors := mtest.CreateCursorResponse(0, "test.botconfig", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		result, err := repo.FindByBotName("nonexistent")
		if err == nil {
			mt.Fatal("expected error for not-found, got nil")
		}
		if result != nil {
			mt.Errorf("expected nil result, got %+v", result)
		}
	})
}

func TestBotConfig_FindActive(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("find active bot configs", func(mt *mtest.T) {
		repo := &BotConfigRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		expected := []BotConfig{
			{ID: primitive.NewObjectID(), BotName: "bot1", BotType: "video", Active: true},
			{ID: primitive.NewObjectID(), BotName: "bot2", BotType: "comment", Active: true},
		}

		first := mtest.CreateCursorResponse(1, "test.bot", mtest.FirstBatch,
			mustMarshalBSON(mt, expected[0]),
			mustMarshalBSON(mt, expected[1]),
		)
		killCursors := mtest.CreateCursorResponse(0, "test.bot", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		results, err := repo.FindActive()
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			mt.Fatalf("expected 2 results, got %d", len(results))
		}
	})
}

func TestBotConfig_Upsert(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("upsert bot config - new", func(mt *mtest.T) {
		repo := &BotConfigRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		mt.AddMockResponses(mtest.CreateSuccessResponse())

		config := &BotConfig{
			BotName: "newbot",
			BotType: "video",
			Active:  true,
			OrgIDs:  []string{"org1"},
			Sleep:   30,
		}
		err := repo.Upsert(config)
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if config.UpdatedAt.IsZero() {
			mt.Error("expected UpdatedAt to be set")
		}
		if config.CreatedAt.IsZero() {
			mt.Error("expected CreatedAt to be set")
		}
	})

	mt.Run("upsert bot config - existing", func(mt *mtest.T) {
		repo := &BotConfigRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		mt.AddMockResponses(mtest.CreateSuccessResponse())

		now := time.Now().Add(-1 * time.Hour)
		config := &BotConfig{
			BotName:   "existingbot",
			BotType:   "video",
			Active:    true,
			CreatedAt: now,
		}
		err := repo.Upsert(config)
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if !config.CreatedAt.Equal(now) {
			mt.Errorf("CreatedAt changed from %v to %v", now, config.CreatedAt)
		}
	})
}

func TestVideoRepository_Upsert(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("upsert video", func(mt *mtest.T) {
		repo := &VideoRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		mt.AddMockResponses(mtest.CreateSuccessResponse())

		video := models.VideoItem{
			SourceURL:   "https://www.tiktok.com/@user1/video/vid-001",
			OrgID:       1,
			VideoID:     "vid-001",
			Description: "test desc",
			PubTime:     1700000000,
			UniqueID:    "user1",
			AuthID:      "auth1",
			AuthName:    "User One",
			Comments:    10,
			Shares:      20,
			Reactions:   30,
			Favors:      5,
			Views:       100,
		}
		ctx := context.Background()
		err := repo.Upsert(ctx, video)
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestVideoRepository_BulkUpsert(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("bulk upsert with videos", func(mt *mtest.T) {
		repo := &VideoRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		mt.AddMockResponses(mtest.CreateSuccessResponse(
			bson.E{Key: "nInserted", Value: int32(1)},
			bson.E{Key: "nModified", Value: int32(0)},
			bson.E{Key: "nUpserted", Value: int32(1)},
			bson.E{Key: "ok", Value: float64(1)},
		))

		videos := []models.VideoItem{
			{
				VideoID:     "v1",
				Description: "desc1",
				UniqueID:    "u1",
				AuthID:      "a1",
				AuthName:    "User1",
			},
		}
		ctx := context.Background()
		err := repo.BulkUpsert(ctx, videos)
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
	})

	mt.Run("bulk upsert empty", func(mt *mtest.T) {
		repo := &VideoRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		ctx := context.Background()
		err := repo.BulkUpsert(ctx, nil)
		if err != nil {
			mt.Fatalf("unexpected error for nil: %v", err)
		}
		err = repo.BulkUpsert(ctx, []models.VideoItem{})
		if err != nil {
			mt.Fatalf("unexpected error for empty: %v", err)
		}
	})
}

func TestVideoRepository_FindBySourceURL(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("find by source url", func(mt *mtest.T) {
		repo := &VideoRepository{
			collection: mt.Coll,
			log:        repoTestLogger(),
		}

		expected := []VideoDocument{
			{
				SourceURL:   "https://www.tiktok.com/@user1/video/vid-001",
				VideoID:     "vid-001",
				Description: "desc",
				PubTime:     1700000000,
				UniqueID:    "user1",
				AuthID:      "auth1",
				AuthName:    "User",
				Comments:    10,
				Shares:      5,
				Reactions:   3,
				Favors:      1,
				Views:       100,
			},
		}

		first := mtest.CreateCursorResponse(1, "test.videos", mtest.FirstBatch,
			mustMarshalBSON(mt, expected[0]),
		)
		killCursors := mtest.CreateCursorResponse(0, "test.videos", mtest.NextBatch)
		mt.AddMockResponses(first, killCursors)

		ctx := context.Background()
		results, err := repo.FindBySourceURL(ctx, "https://www.tiktok.com/@user1/video/vid-001", 10)
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			mt.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].VideoID != "vid-001" {
			mt.Errorf("VideoID = %q, want %q", results[0].VideoID, "vid-001")
		}
	})
}

func TestURLModel(t *testing.T) {
	id := primitive.NewObjectID()
	u := URLEntry{
		ID:     id,
		URL:    "https://www.tiktok.com/@testuser",
		OrgID:  42,
		Active: true,
	}
	if u.URL != "https://www.tiktok.com/@testuser" {
		t.Errorf("URL = %q, want %q", u.URL, "https://www.tiktok.com/@testuser")
	}
	if u.OrgID != 42 {
		t.Errorf("OrgID = %d, want 42", u.OrgID)
	}
	if !u.Active {
		t.Error("expected Active=true")
	}
	if u.ID != id {
		t.Error("ID mismatch")
	}
}

func TestBotConfigModel(t *testing.T) {
	id := primitive.NewObjectID()
	now := time.Now()
	bc := BotConfig{
		ID:        id,
		BotName:   "testbot",
		BotType:   "video",
		OrgIDs:    []string{"org1", "org2"},
		Sleep:     30,
		GPMAPI:    "https://gpm.example.com",
		ProfileID: "profile-001",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if bc.BotName != "testbot" {
		t.Errorf("BotName = %q, want %q", bc.BotName, "testbot")
	}
	if bc.BotType != "video" {
		t.Errorf("BotType = %q, want %q", bc.BotType, "video")
	}
	if len(bc.OrgIDs) != 2 {
		t.Errorf("OrgIDs len = %d, want 2", len(bc.OrgIDs))
	}
	if bc.Sleep != 30 {
		t.Errorf("Sleep = %d, want 30", bc.Sleep)
	}
}

func TestVideoDocumentModel(t *testing.T) {
	vd := VideoDocument{
		SourceURL:   "https://www.tiktok.com/@user1/video/vid-1",
		VideoID:     "vid-1",
		Description: "desc",
		PubTime:     1700000000,
		UniqueID:    "user1",
		AuthID:      "auth1",
		AuthName:    "User",
		Comments:    10,
		Shares:      5,
		Reactions:   3,
		Favors:      1,
		Views:       100,
	}
	if vd.SourceURL != "https://www.tiktok.com/@user1/video/vid-1" {
		t.Errorf("SourceURL = %q, want %q", vd.SourceURL, "https://www.tiktok.com/@user1/video/vid-1")
	}
	if vd.VideoID != "vid-1" {
		t.Errorf("VideoID = %q, want %q", vd.VideoID, "vid-1")
	}
	if vd.Comments != 10 {
		t.Errorf("Comments = %d, want 10", vd.Comments)
	}
}

func TestOrg_FindActiveOrgIDs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		if err != nil {
			t.Fatalf("failed to create pgxmock pool: %v", err)
		}
		defer mock.Close()

		repo := &OrgRepository{db: mock}

		rows := pgxmock.NewRows([]string{"org_id"}).AddRow(1).AddRow(2)
		mock.ExpectQuery(regexp.QuoteMeta(findActiveOrgIDsQuery)).WillReturnRows(rows)

		orgIDs, err := repo.FindActiveOrgIDs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(orgIDs) != 2 || orgIDs[0] != 1 || orgIDs[1] != 2 {
			t.Errorf("orgIDs = %v, want [1 2]", orgIDs)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	})

	t.Run("empty results", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		if err != nil {
			t.Fatalf("failed to create pgxmock pool: %v", err)
		}
		defer mock.Close()

		repo := &OrgRepository{db: mock}

		rows := pgxmock.NewRows([]string{"org_id"})
		mock.ExpectQuery(regexp.QuoteMeta(findActiveOrgIDsQuery)).WillReturnRows(rows)

		orgIDs, err := repo.FindActiveOrgIDs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(orgIDs) != 0 {
			t.Errorf("expected 0 org IDs, got %d", len(orgIDs))
		}
	})

	t.Run("query error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		if err != nil {
			t.Fatalf("failed to create pgxmock pool: %v", err)
		}
		defer mock.Close()

		repo := &OrgRepository{db: mock}

		mock.ExpectQuery(regexp.QuoteMeta(findActiveOrgIDsQuery)).WillReturnError(fmt.Errorf("connection reset"))

		if _, err := repo.FindActiveOrgIDs(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

// mustMarshalBSON marshals a value to BSON and returns the raw bytes.
func mustMarshalBSON(t testing.TB, v interface{}) bson.D {
	data, err := bson.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal BSON: %v", err)
	}
	var doc bson.D
	if err := bson.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to unmarshal BSON: %v", err)
	}
	return doc
}
