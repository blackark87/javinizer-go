package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCanonicalizeDescriptiveActresses(t *testing.T) {
	agg := &Aggregator{}

	blurb := "【あいちゃん/24歳/173cm！！超美巨Iカップのガチ美女OL！！】【のんちゃん/22歳/Gカップの美爆乳OL！！】神スタイル美女2人の大乱れ！！一挙配信SP！！"
	in := []models.Actress{
		{JapaneseName: blurb},
		{JapaneseName: "【あいちゃん"}, // second blurb fragment
		{JapaneseName: "波多野結衣"},  // real name, must survive
	}

	out := agg.canonicalizeDescriptiveActresses(in)

	// The two blurbs collapse into a single Unknown; the real name is preserved.
	assert.Len(t, out, 2)
	assert.True(t, models.IsUnknownActressFields(out[0].LastName, out[0].FirstName, out[0].JapaneseName),
		"first entry should be Unknown")
	assert.Equal(t, "波多野結衣", out[1].JapaneseName, "real actress name should be preserved")
}
