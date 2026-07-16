package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressLookupRepository adapts the context-aware content repository to the
// aggregator's narrow read-only lookup contract.
type ActressLookupRepository struct {
	repo *ActressRepository
}

// NewActressLookupRepository creates the read-only actress lookup adapter.
func NewActressLookupRepository(db *DB) *ActressLookupRepository {
	if db == nil {
		return nil
	}
	return &ActressLookupRepository{repo: NewActressRepository(db)}
}

// FindByDMMID finds an actress by the provider's stable DMM identifier.
func (r *ActressLookupRepository) FindByDMMID(dmmID int) (*models.Actress, error) {
	if r == nil || r.repo == nil {
		return nil, ErrNotFound
	}
	return r.repo.FindByDMMID(context.Background(), dmmID)
}

// FindUnverifiedByJapaneseName only reuses name-only records.  A record tied
// to a positive DMM ID is not safe to reuse for another performer with the
// same stage name.
func (r *ActressLookupRepository) FindUnverifiedByJapaneseName(name string) (*models.Actress, error) {
	if r == nil || r.repo == nil || strings.TrimSpace(name) == "" {
		return nil, ErrNotFound
	}
	var actress models.Actress
	err := r.repo.GetDB().WithContext(context.Background()).
		Where("japanese_name = ? AND dmm_id <= 0", strings.TrimSpace(name)).
		Order("id ASC").First(&actress).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("unverified actress %s", name), err)
	}
	return &actress, nil
}
