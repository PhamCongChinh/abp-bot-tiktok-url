package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrgStore abstracts organisation lookup operations.
// Define at consumer side so Crawler depends on this, not on *OrgRepository.
type OrgStore interface {
	FindActiveOrgIDs() ([]int, error)
}

// pgQuerier is the subset of *pgxpool.Pool used by OrgRepository — satisfied
// by the real pool in production and by pgxmock in tests.
type pgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const findActiveOrgIDsQuery = `SELECT t.org_id, t.name FROM tbl_org t WHERE t.status = 'ACTIVE'`

type OrgRepository struct {
	db pgQuerier
}

func NewOrgRepository(pool *pgxpool.Pool) *OrgRepository {
	return &OrgRepository{db: pool}
}

// FindActiveOrgIDs returns the org_id of every row in `tbl_org` with
// status = 'ACTIVE'.
func (r *OrgRepository) FindActiveOrgIDs() ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := r.db.Query(ctx, findActiveOrgIDsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgIDs []int
	for rows.Next() {
		var orgID int
		var name string
		if err := rows.Scan(&orgID, &name); err != nil {
			return nil, err
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return orgIDs, nil
}
