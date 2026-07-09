package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestBuildScrapeCandidates(t *testing.T) {
	t.Run("same title -> no conflict", func(t *testing.T) {
		res := []*models.ScraperResult{
			{Source: "javbus", ID: "MAG-001", Title: "Aki Sasaki", Actresses: []models.ActressInfo{{}}},
			{Source: "r18dev", ID: "MAG-001", Title: "aki  sasaki"},
		}
		cands, conflict := buildScrapeCandidates(res)
		if len(cands) != 2 {
			t.Fatalf("want 2 candidates, got %d", len(cands))
		}
		if conflict {
			t.Fatal("expected no conflict for same normalized title")
		}
		if cands[0].Source != "javbus" || cands[0].ActressCount != 1 {
			t.Fatalf("bad candidate summary: %+v", cands[0])
		}
	})

	t.Run("different title -> conflict", func(t *testing.T) {
		res := []*models.ScraperResult{
			{Source: "javbus", Title: "AV女優のホントのSEX見せて下さい AIKA"},
			{Source: "r18dev", Title: "Aki Sasaki"},
		}
		_, conflict := buildScrapeCandidates(res)
		if !conflict {
			t.Fatal("expected conflict for different titles")
		}
	})

	t.Run("single result -> no conflict", func(t *testing.T) {
		res := []*models.ScraperResult{{Source: "dmm", Title: "X"}}
		_, conflict := buildScrapeCandidates(res)
		if conflict {
			t.Fatal("single result cannot conflict")
		}
	})
}
