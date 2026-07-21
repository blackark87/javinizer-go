package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsDescriptiveNonName(t *testing.T) {
	tests := []struct {
		name                  string
		last, first, japanese string
		want                  bool
	}{
		{"real kanji name", "", "", "波多野結衣", false},
		{"real kana nickname", "", "", "あいちゃん", false},
		{"real kanji name 2", "", "", "藤原絵理香", false},
		{"romaji name", "Hatano", "Yui", "", false},
		{"blurb with brackets/age/cup", "", "", "【あいちゃん/24歳/173cm！！超美巨Iカップのガチ美女OL！！】【のんちゃん/22歳/Gカップの美爆乳OL！！】神スタイル美女2人の大乱れ！！一挙配信SP！！", true},
		{"short bracket blurb", "", "", "【あいちゃん", true},
		{"age marker only", "", "", "カレン25歳", true},
		{"cup marker only", "", "", "みおGカップ", true},
		// relation/occupation description with no name → non-name
		{"celeb wife description", "", "", "欲求不満セレブ妻", true},
		{"lounge occupation", "", "", "西麻布ラウンジ勤務", true},
		{"married woman", "", "", "人妻", true},
		{"appearance and personality blurb", "", "", "高飛車でプライドの高い美しい美女", true},
		{"real name愛梨沙 stays a name", "", "", "愛梨沙", false},
		{"real kana name あいり stays a name", "", "", "あいり", false},
		{"20 runes is within limit", "", "", "あいうえおかきくけこさしすせそたちつてと", false}, // exactly 20 runes, not > 20
		{"over 20 runes flagged", "", "", "あいうえおかきくけこさしすせそたちつてとなに", true},   // 22 runes > 20
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDescriptiveNonName(tt.last, tt.first, tt.japanese)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsUnknownActressName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"english", "Unknown", true},
		{"english actor phrase", "unknown actor", true},
		{"korean unknown", "미상", true},
		{"korean no spaces", "알수없음", true},
		{"korean spaced", "알 수 없음", true},
		{"normal name", "Hatano Yui", false},
		{"empty", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsUnknownActressName(tc.in))
		})
	}
}

func TestCanonicalizeUnknownActress(t *testing.T) {
	actress := &Actress{FirstName: "미상", LastName: "ignored", JapaneseName: "미상"}

	assert.False(t, CanonicalizeUnknownActress(actress))
	assert.Equal(t, "미상", actress.FirstName)
	assert.Equal(t, "ignored", actress.LastName)
	assert.Equal(t, "미상", actress.JapaneseName)

	actress = &Actress{FirstName: "미상", JapaneseName: "알 수 없음"}

	assert.True(t, CanonicalizeUnknownActress(actress))
	assert.Equal(t, UnknownActressName, actress.FirstName)
	assert.Empty(t, actress.LastName)
	assert.Equal(t, UnknownActressName, actress.JapaneseName)
}

func TestApplyUnknownActressMode(t *testing.T) {
	t.Run("fallback adds canonical actress to empty cast", func(t *testing.T) {
		movie := &Movie{}

		changed := ApplyUnknownActressMode(movie, UnknownActressModeFallback, "Unknown")

		assert.True(t, changed)
		require.Len(t, movie.Actresses, 1)
		assert.Equal(t, UnknownActressName, movie.Actresses[0].FirstName)
		assert.Equal(t, UnknownActressName, movie.Actresses[0].JapaneseName)
	})

	t.Run("fallback preserves one existing placeholder identity", func(t *testing.T) {
		movie := &Movie{Actresses: []Actress{{ID: 42, FirstName: "미상", JapaneseName: "알 수 없음"}}}

		changed := ApplyUnknownActressMode(movie, UnknownActressModeFallback, "Unknown")

		assert.True(t, changed)
		require.Len(t, movie.Actresses, 1)
		assert.Equal(t, uint(42), movie.Actresses[0].ID)
		assert.Equal(t, UnknownActressName, movie.Actresses[0].FirstName)
		assert.Equal(t, UnknownActressName, movie.Actresses[0].JapaneseName)
	})

	t.Run("fallback preserves placeholder alongside real actress", func(t *testing.T) {
		movie := &Movie{Actresses: []Actress{
			{FirstName: UnknownActressName, JapaneseName: UnknownActressName},
			{FirstName: "레나", LastName: "미야시타", JapaneseName: "宮下玲奈"},
		}}

		changed := ApplyUnknownActressMode(movie, UnknownActressModeFallback, "Unknown")

		assert.False(t, changed)
		require.Len(t, movie.Actresses, 2)
		assert.Equal(t, UnknownActressName, movie.Actresses[0].JapaneseName)
		assert.Equal(t, "宮下玲奈", movie.Actresses[1].JapaneseName)
	})

	t.Run("fallback collapses duplicate placeholders", func(t *testing.T) {
		movie := &Movie{Actresses: []Actress{
			{FirstName: "미상"},
			{FirstName: UnknownActressName, JapaneseName: UnknownActressName},
		}}

		changed := ApplyUnknownActressMode(movie, UnknownActressModeFallback, "Unknown")

		assert.True(t, changed)
		require.Len(t, movie.Actresses, 1)
		assert.Equal(t, UnknownActressName, movie.Actresses[0].FirstName)
	})

	t.Run("skip removes placeholder", func(t *testing.T) {
		movie := &Movie{Actresses: []Actress{{FirstName: UnknownActressName, JapaneseName: UnknownActressName}}}

		changed := ApplyUnknownActressMode(movie, UnknownActressModeSkip, "Unknown")

		assert.True(t, changed)
		assert.Empty(t, movie.Actresses)
	})
}
