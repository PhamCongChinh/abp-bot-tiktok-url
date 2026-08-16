package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// PostgresDB wraps a pgx connection pool for querying PostgreSQL.
type PostgresDB struct {
	Pool *pgxpool.Pool
}

// NewPostgresDB opens a pgx connection pool for dsn and verifies connectivity
// with a ping before returning.
func NewPostgresDB(ctx context.Context, dsn string, log *zap.Logger) (*PostgresDB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	log.Info("PostgreSQL connected")

	return &PostgresDB{Pool: pool}, nil
}

// Close releases all connections in the pool.
func (p *PostgresDB) Close() {
	p.Pool.Close()
}
