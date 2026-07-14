package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// fakeActressLookup is an in-memory ActressLookupRepositoryInterface.
type fakeActressLookup struct {
	byJa    map[string]models.Actress
	byDMMID map[int]models.Actress
	calls   int
}

func (f *fakeActressLookup) FindByDMMID(dmmID int) (*models.Actress, error) {
	f.calls++
	if a, ok := f.byDMMID[dmmID]; ok {
		return &a, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeActressLookup) FindUnverifiedByJapaneseName(name string) (*models.Actress, error) {
	f.calls++
	if a, ok := f.byJa[name]; ok && a.DMMID <= 0 {
		return &a, nil
	}
	return nil, database.ErrNotFound
}

func TestEnrichActressReadings(t *testing.T) {
	newAgg := func(lookup database.ActressLookupRepositoryInterface) *Aggregator {
		return &Aggregator{actressLookupRepo: lookup}
	}

	t.Run("kanji-only actress is enriched from DB by Japanese name", func(t *testing.T) {
		lookup := &fakeActressLookup{
			byJa: map[string]models.Actress{
				"伊藤舞雪": {JapaneseName: "伊藤舞雪", FirstName: "Mayuki", LastName: "Ito", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/ito_mayuki.jpg"},
			},
		}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: "伊藤舞雪"}}

		agg.enrichActressReadings(actresses)

		assert.Equal(t, "Mayuki", actresses[0].FirstName)
		assert.Equal(t, "Ito", actresses[0].LastName)
		assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/ito_mayuki.jpg", actresses[0].ThumbURL)
	})

	t.Run("DMMID lookup preferred when available", func(t *testing.T) {
		lookup := &fakeActressLookup{
			byDMMID: map[int]models.Actress{
				42: {DMMID: 42, FirstName: "Mayuki", LastName: "Ito"},
			},
		}
		agg := newAgg(lookup)
		actresses := []models.Actress{{DMMID: 42, JapaneseName: "伊藤舞雪"}}

		agg.enrichActressReadings(actresses)

		assert.Equal(t, "Mayuki", actresses[0].FirstName)
		assert.Equal(t, "Ito", actresses[0].LastName)
	})

	t.Run("name-only lookup does not reuse a positive DMM profile", func(t *testing.T) {
		lookup := &fakeActressLookup{
			byJa: map[string]models.Actress{
				"同名女優": {DMMID: 99, JapaneseName: "同名女優", FirstName: "Verified"},
			},
		}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: "同名女優"}}

		agg.enrichActressReadings(actresses)

		assert.Empty(t, actresses[0].FirstName)
	})

	t.Run("actress with romaji is not looked up", func(t *testing.T) {
		lookup := &fakeActressLookup{
			byJa: map[string]models.Actress{
				"伊藤舞雪": {FirstName: "WRONG", LastName: "WRONG"},
			},
		}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: "伊藤舞雪", FirstName: "Mayuki", LastName: "Ito"}}

		agg.enrichActressReadings(actresses)

		assert.Equal(t, 0, lookup.calls, "should not query DB when a reading already exists")
		assert.Equal(t, "Mayuki", actresses[0].FirstName)
	})

	t.Run("actress with actjpgs thumb is not looked up", func(t *testing.T) {
		lookup := &fakeActressLookup{byJa: map[string]models.Actress{"x": {}}}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: "x", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/foo.jpg"}}

		agg.enrichActressReadings(actresses)
		assert.Equal(t, 0, lookup.calls)
	})

	t.Run("already-Hangul actress is not looked up", func(t *testing.T) {
		lookup := &fakeActressLookup{byJa: map[string]models.Actress{"伊藤舞雪": {FirstName: "WRONG"}}}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: "伊藤舞雪", LastName: "이토", FirstName: "마유키"}}

		agg.enrichActressReadings(actresses)
		assert.Equal(t, 0, lookup.calls)
		assert.Equal(t, "마유키", actresses[0].FirstName)
	})

	t.Run("only empty fields are filled", func(t *testing.T) {
		lookup := &fakeActressLookup{
			byJa: map[string]models.Actress{
				"愛田": {FirstName: "Fromdb", LastName: "Fromdb", ThumbURL: "keep-existing?no"},
			},
		}
		agg := newAgg(lookup)
		// LastName present (kanji), FirstName empty → only FirstName filled.
		actresses := []models.Actress{{JapaneseName: "愛田", LastName: "愛田"}}

		agg.enrichActressReadings(actresses)
		assert.Equal(t, "Fromdb", actresses[0].FirstName)
		assert.Equal(t, "愛田", actresses[0].LastName, "existing LastName must not be overwritten")
	})

	t.Run("unknown actress is skipped", func(t *testing.T) {
		lookup := &fakeActressLookup{byJa: map[string]models.Actress{models.UnknownActressName: {FirstName: "x"}}}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: models.UnknownActressName, FirstName: models.UnknownActressName}}

		agg.enrichActressReadings(actresses)
		assert.Equal(t, 0, lookup.calls)
	})

	t.Run("no repo is a no-op", func(t *testing.T) {
		agg := newAgg(nil)
		actresses := []models.Actress{{JapaneseName: "伊藤舞雪"}}
		agg.enrichActressReadings(actresses) // must not panic
		assert.Equal(t, "", actresses[0].FirstName)
	})

	t.Run("not found in DB leaves actress unchanged", func(t *testing.T) {
		lookup := &fakeActressLookup{byJa: map[string]models.Actress{}}
		agg := newAgg(lookup)
		actresses := []models.Actress{{JapaneseName: "未知"}}
		agg.enrichActressReadings(actresses)
		assert.Equal(t, "", actresses[0].FirstName)
	})
}
