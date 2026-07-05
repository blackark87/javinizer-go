package translation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// =============================================================================
// normalizeProvider tests
// =============================================================================

func TestNormalizeProvider(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase",
			input:    "openai",
			expected: "openai",
		},
		{
			name:     "uppercase",
			input:    "OPENAI",
			expected: "openai",
		},
		{
			name:     "with leading whitespace",
			input:    "  deepl",
			expected: "deepl",
		},
		{
			name:     "with trailing whitespace",
			input:    "google  ",
			expected: "google",
		},
		{
			name:     "with surrounding whitespace",
			input:    "  openai  ",
			expected: "openai",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "mixed case",
			input:    "GoOgLe",
			expected: "google",
		},
		{
			name:     "unknown provider",
			input:    "CustomProvider",
			expected: "customprovider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeProvider(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// normalizeLanguage tests
// =============================================================================

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase",
			input:    "en",
			expected: "en",
		},
		{
			name:     "uppercase",
			input:    "EN",
			expected: "en",
		},
		{
			name:     "with leading whitespace",
			input:    "  ja",
			expected: "ja",
		},
		{
			name:     "with trailing whitespace",
			input:    "zh  ",
			expected: "zh",
		},
		{
			name:     "with surrounding whitespace",
			input:    "  ko  ",
			expected: "ko",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "language with region",
			input:    "en-US",
			expected: "en-us",
		},
		{
			name:     "language with underscore",
			input:    "pt_BR",
			expected: "pt_br",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLanguage(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// =============================================================================
// replaceActressName tests
// =============================================================================

func TestReplaceActressName(t *testing.T) {
	tests := []struct {
		name       string
		actress    *models.Actress
		translated string
		expected   models.Actress
	}{
		{
			name: "japanese actress - preserves JapaneseName, FamilyName GivenName order",
			actress: &models.Actress{
				JapaneseName: "田中香",
			},
			translated: "Tanaka Yui",
			expected: models.Actress{
				JapaneseName: "田中香",
				LastName:     "Tanaka",
				FirstName:    "Yui",
			},
		},
		{
			name: "actress with existing english name - FamilyName GivenName order",
			actress: &models.Actress{
				FirstName: "Yui",
				LastName:  "Tanaka",
			},
			translated: "Tanaka Yui",
			expected: models.Actress{
				JapaneseName: "",
				LastName:     "Tanaka",
				FirstName:    "Yui",
			},
		},
		{
			name: "empty translated - no change",
			actress: &models.Actress{
				JapaneseName: "田中香",
			},
			translated: "",
			expected: models.Actress{
				JapaneseName: "田中香",
			},
		},
		{
			name:       "single word name - stored in FirstName only",
			actress:    &models.Actress{},
			translated: "Serina",
			expected: models.Actress{
				FirstName: "Serina",
				LastName:  "",
			},
		},
		{
			name: "three word name - first word is LastName, rest is FirstName",
			actress: &models.Actress{
				JapaneseName: "田中",
			},
			translated: "De Niro Maria",
			expected: models.Actress{
				JapaneseName: "田中",
				LastName:     "De",
				FirstName:    "Niro Maria",
			},
		},
		{
			name:       "whitespace trimmed before splitting",
			actress:    &models.Actress{},
			translated: "  Tanaka Yui  ",
			expected: models.Actress{
				LastName:  "Tanaka",
				FirstName: "Yui",
			},
		},
		{
			name: "parenthetical content stripped from output",
			actress: &models.Actress{
				JapaneseName: "黒木麻衣",
			},
			translated: "Kuroki Mai(Mai",
			expected: models.Actress{
				JapaneseName: "黒木麻衣",
				LastName:     "Kuroki",
				FirstName:    "Mai",
			},
		},
		{
			name: "parenthetical-only becomes empty - no change",
			actress: &models.Actress{
				JapaneseName: "田中香",
				FirstName:    "OldFirst",
				LastName:     "OldLast",
			},
			translated: "(Mai",
			expected: models.Actress{
				JapaneseName: "田中香",
				FirstName:    "OldFirst",
				LastName:     "OldLast",
			},
		},
		{
			name:       "full-width parenthesis stripped",
			actress:    &models.Actress{},
			translated: "Kuroki Mai（extra",
			expected: models.Actress{
				LastName:  "Kuroki",
				FirstName: "Mai",
			},
		},
		{
			name:       "long vowels normalized to ASCII",
			actress:    &models.Actress{JapaneseName: "波多野結衣"},
			translated: "Hatano Yūi",
			expected: models.Actress{
				JapaneseName: "波多野結衣",
				LastName:     "Hatano",
				FirstName:    "Yui",
			},
		},
		{
			name:       "multiple long vowels normalized",
			actress:    &models.Actress{},
			translated: "Ōshima Māi",
			expected: models.Actress{
				LastName:  "Oshima",
				FirstName: "Mai",
			},
		},
		{
			name: "Korean LLM output applied - single Hangul name in FirstName",
			actress: &models.Actress{
				JapaneseName: "まひる",
				FirstName:    "Mahiru",
				LastName:     "",
			},
			translated: "마히루",
			expected: models.Actress{
				JapaneseName: "まひる",
				FirstName:    "마히루",
				LastName:     "",
			},
		},
		{
			name: "Korean LLM output applied - FamilyName GivenName order",
			actress: &models.Actress{
				JapaneseName: "波多野結衣",
				FirstName:    "Yui",
				LastName:     "Hatano",
			},
			translated: "하타노 유이",
			expected: models.Actress{
				JapaneseName: "波多野結衣",
				FirstName:    "유이",
				LastName:     "하타노",
			},
		},
		{
			name: "CJK LLM output ignored - actress unchanged",
			actress: &models.Actress{
				JapaneseName: "田中香",
				FirstName:    "Yui",
				LastName:     "Tanaka",
			},
			translated: "田中香",
			expected: models.Actress{
				JapaneseName: "田中香",
				FirstName:    "Yui",
				LastName:     "Tanaka",
			},
		},
	}

	// Test nil actress directly for nil-safety branch
	t.Run("nil actress direct", func(t *testing.T) {
		assert.NotPanics(t, func() {
			replaceActressName(nil, "Test")
		})
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy for comparison
			actressCopy := &models.Actress{}
			if tt.actress != nil {
				*actressCopy = *tt.actress
			}

			replaceActressName(actressCopy, tt.translated)

			assert.Equal(t, tt.expected.JapaneseName, actressCopy.JapaneseName)
			assert.Equal(t, tt.expected.FirstName, actressCopy.FirstName)
			assert.Equal(t, tt.expected.LastName, actressCopy.LastName)
		})
	}
}

// =============================================================================
// cleanActressNameForTranslation tests
// =============================================================================

func TestCleanActressNameForTranslation(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// no-op cases
		{"カレン", "カレン"},
		{"田中香", "田中香"},
		{"まひる", "まひる"},
		{"", ""},
		// bracket stripping (existing)
		{"[カレン]", "カレン"},
		{"[田中香]", "田中香"},
		// comma stripping (existing)
		{"カレン, 25歳, 歯科衛生士", "カレン"},
		// middle-dot stripping (new)
		{"りむ・Hカップ 20歳 コンカフェ店員", "りむ"},
		{"りむ・Hカップ", "りむ"},
		// age suffix stripping + honorific removal (honorifics are now stripped)
		{"カレン 25歳 歯科衛生士", "カレン"},
		{"ひとみさん 27歳 探偵", "ひとみ"},
		{"あおいちゃん 22歳 地下アイドル", "あおい"},
		{"ミウちゃん 22歳 職業不詳SSS級ギャル", "ミウ"},
		{"まひるちゃん 22歳 美容師のアシスタント", "まひる"},
		{"カレン ２５歳 歯科衛生士", "カレン"}, // full-width digits
		// honorific-based name extraction from trailing token, then honorific stripped
		{"高身長172cmショート× Gカップ豹変アクメギャル メイちゃん", "メイ"},
		{"デカパイ美容師 ひとみさん", "ひとみ"},
		// trailing honorific on a bare single-token name is stripped
		{"ありささん", "ありさ"},
		{"あいちゃん", "あい"},
		// occupation/location descriptor token stripped, name kept
		{"愛梨沙 西麻布ラウンジ勤務", "愛梨沙"},
		// entirely description — no extraction possible, pass through as-is
		{"某高級ホテル従業員", "某高級ホテル従業員"},
		{"植え込みに頭突っ込んでたデカパイ女", "植え込みに頭突っ込んでたデカパイ女"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanActressNameForTranslation(tt.input))
		})
	}
}

// =============================================================================
// cleanDescriptionForTranslation tests
// =============================================================================

func TestCleanDescriptionForTranslation(t *testing.T) {
	promoBlock := "※この作品はバイノーラル録音されておりますが、視点移動により音声が連動するものではありません。 ※この商品は専用プレイヤーでの視聴に最適化されています。 ※VR専用作品は必ず下記リンクより動作環境・対応デバイスを確認いただきご購入ください。 「動作環境・対応デバイス」について ※ 配信方法によって収録内容が異なる場合があります。 特集 最新作やセール商品など、お得な情報満載의 『【VR】KMPストア』はこちら！"
	doubleStorePromoBlock := "※この作品はバイノーラル録音されておりますが、視点移動により音声が連動するものではありません。 ※この商品は専用プレイヤーでの視聴に最適化されています。 ※VR専用作品は必ず下記リンクより動作環境・対応デバイスを確認いただきご購入ください。 「動作環境・対応デバイス」について ※ 配信方法によって収録内容が異なる場合があります。 特集 最新作やセール商品など、お得な情報満載의 『【厳選】KMPストア 2号店』はこちら！最新作やセール商品など、お得な情報満載의 『【VR】KMPストア』はこちら！"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "promo-only VR notice is removed",
			input:    promoBlock,
			expected: "",
		},
		{
			name:     "double KMP store promo is removed",
			input:    doubleStorePromoBlock,
			expected: "",
		},
		{
			name:     "main description before promo is preserved",
			input:    "本編の説明です。 " + promoBlock,
			expected: "本編の説明です。",
		},
		{
			name:     "normal description is preserved",
			input:    "彼女との甘い時間を描いた本編の説明です。",
			expected: "彼女との甘い時間を描いた本編の説明です。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanDescriptionForTranslation(tt.input))
		})
	}
}

func TestCleanTitleForTranslation(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"【VR】森日向子", "森日向子"},
		{"[VR] 모리 히나코", "모리 히나코"},
		{"【8K VR】素敵なタイトル", "素敵なタイトル"},
		{"彼女とVRデート", "彼女とVRデート"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanTitleForTranslation(tt.input))
		})
	}
}

// =============================================================================
// stripVRMarkers tests
// =============================================================================

func TestStripVRMarkers(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// bracketed VR tags removed
		{"[VR] 素敵なタイトル", "素敵なタイトル"},
		{"【VR】素敵なタイトル", "素敵なタイトル"},
		{"【8K VR】タイトル", "タイトル"},
		{"タイトル [4K VR]", "タイトル"},
		{"タイトル（VR）", "タイトル"},
		{"［VR］タイトル", "タイトル"},
		{"［ＶＲ］タイトル", "タイトル"},
		{"【VR専用】タイトル", "タイトル"},
		{"【VR作品】タイトル", "タイトル"},
		{"[8Ｋ　ＶＲ] タイトル", "タイトル"},
		{"[vr] lowercase tag", "lowercase tag"},
		{"タイトル前半 [VR] 後半", "タイトル前半 後半"},
		// title that is only a VR tag becomes empty
		{"[VR]", ""},
		{"【8K VR】", ""},
		// bare "VR" in the title text is kept
		{"VR初体験のタイトル", "VR初体験のタイトル"},
		{"彼女とVRデート", "彼女とVRデート"},
		// mixed: tag removed, in-text VR kept
		{"[VR] VRの世界へ", "VRの世界へ"},
		// non-VR brackets untouched
		{"[中出し] タイトル", "[中出し] タイトル"},
		// full-width space preserved when no tag present
		{"タイトル　続編", "タイトル　続編"},
		// no-op cases
		{"素敵なタイトル", "素敵なタイトル"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripVRMarkers(tt.input))
		})
	}
}

// =============================================================================
// stripPromoMarkers tests
// =============================================================================

func TestStripPromoMarkers(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// bracketed promo tags removed
		{"[FANZA 限定] 素敵なタイトル", "素敵なタイトル"},
		{"[数量限定] タイトル", "タイトル"},
		{"【期間限定セール】タイトル", "タイトル"},
		{"タイトル【特典映像付き】", "タイトル"},
		{"（DMM独占）タイトル", "タイトル"},
		{"[30%割引] タイトル", "タイトル"},
		{"[FANZA 限定][数量限定] タイトル", "タイトル"},
		// meaningful brackets are kept
		{"【シリーズ名】タイトル", "【シリーズ名】タイトル"},
		{"[中出し] タイトル", "[中出し] タイトル"},
		// promo keyword outside brackets is kept (part of the title text)
		{"限定公開のタイトル", "限定公開のタイトル"},
		// no-op cases
		{"素敵なタイトル", "素敵なタイトル"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripPromoMarkers(tt.input))
		})
	}
}

// =============================================================================
// isLikelyRomanized tests
// =============================================================================

func TestIsLikelyRomanized(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Mahiru", true},
		{"Tanaka Yui", true},
		{"De Niro Maria", true},
		{"Futabareena", true},
		{"Oshima", true},
		{"", true},
		{"마히루", false},
		{"대낮", false},
		{"まひる", false},
		{"田中香", false},
		{"Mahiru 마히루", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, isLikelyRomanized(tt.input))
		})
	}
}

// =============================================================================
// extractNamesFromDMMActjpgsURL tests
// =============================================================================

func TestExtractNamesFromDMMActjpgsURL(t *testing.T) {
	tests := []struct {
		name          string
		thumbURL      string
		wantLastName  string
		wantFirstName string
		wantOk        bool
	}{
		{
			name:          "standard format - nihonshiki si→shi",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/miyosi_yuka.jpg",
			wantLastName:  "miyoshi",
			wantFirstName: "yuka",
			wantOk:        true,
		},
		{
			name:          "trailing number stripped - nihonshiki si→shi",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/miyasita_rena2.jpg",
			wantLastName:  "miyashita",
			wantFirstName: "rena",
			wantOk:        true,
		},
		{
			name:          "trailing underscore+number stripped",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/ito_mayuki_2.jpg",
			wantLastName:  "ito",
			wantFirstName: "mayuki",
			wantOk:        true,
		},
		{
			name:     "no actjpgs prefix - not matched",
			thumbURL: "https://pics.dmm.co.jp/mono/other/miyosi_yuka.jpg",
			wantOk:   false,
		},
		{
			name:     "empty url",
			thumbURL: "",
			wantOk:   false,
		},
		{
			name:          "single name part - matched as first name only",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/yuka.jpg",
			wantLastName:  "",
			wantFirstName: "yuka",
			wantOk:        true,
		},
		{
			name:          "single name with trailing digits stripped",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/reimi21.jpg",
			wantLastName:  "",
			wantFirstName: "reimi",
			wantOk:        true,
		},
		{
			name:          "query string ignored",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/tanaka_yui.jpg?v=123",
			wantLastName:  "tanaka",
			wantFirstName: "yui",
			wantOk:        true,
		},
		{
			name:          "nihonshiki ti→chi",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/itinose_ameri.jpg",
			wantLastName:  "ichinose",
			wantFirstName: "ameri",
			wantOk:        true,
		},
		{
			name:          "nihonshiki tu→tsu",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/tutumi_rei.jpg",
			wantLastName:  "tsutsumi",
			wantFirstName: "rei",
			wantOk:        true,
		},
		{
			name:          "nihonshiki tu+zi→tsu+ji",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/tuzi_kana.jpg",
			wantLastName:  "tsuji",
			wantFirstName: "kana",
			wantOk:        true,
		},
		{
			name:          "no nihonshiki - unchanged",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/tanaka_yuka.jpg",
			wantLastName:  "tanaka",
			wantFirstName: "yuka",
			wantOk:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastName, firstName, ok := extractNamesFromDMMActjpgsURL(tt.thumbURL)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantLastName, lastName)
				assert.Equal(t, tt.wantFirstName, firstName)
			}
		})
	}
}

func TestParseLLMTranslationPayload(t *testing.T) {
	t.Run("parses compact marked output with embedded quotes", func(t *testing.T) {
		input := `<<<JZ_0>>>
Karen
<<<JZ_1>>>
She says "It's forceful..." but looks happy while being teased.`

		got, err := parseLLMTranslationPayload(input, []string{"<<<JZ_0>>>", "<<<JZ_1>>>"})
		require.NoError(t, err)
		assert.Equal(t, []string{
			"Karen",
			`She says "It's forceful..." but looks happy while being teased.`,
		}, got)
	})

	t.Run("errors when first marker is missing", func(t *testing.T) {
		_, err := parseLLMTranslationPayload(`["hello","world"]`, []string{"<<<JZ_0>>>", "<<<JZ_1>>>"})
		require.Error(t, err)
	})
}

// =============================================================================
// parseGoogleFreeResponse tests
// =============================================================================

func TestParseGoogleFreeResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
		wantErr  bool
	}{
		{
			name:     "valid response",
			input:    []byte(`[[["Hello world",null,"en",null,null,null,"gtx"]]]`),
			expected: "Hello world",
			wantErr:  false,
		},
		{
			name:     "multiple segments",
			input:    []byte(`[[["Hello ",null,"en",null,null,null,"gtx"],["world",null,"en",null,null,null,"gtx"]]]`),
			expected: "Hello world",
			wantErr:  false,
		},
		{
			name:     "invalid top level structure",
			input:    []byte(`{"not":"array"}`),
			expected: "",
			wantErr:  true,
		},
		{
			name:     "empty array",
			input:    []byte(`[]`),
			expected: "",
			wantErr:  true,
		},
		{
			name:     "invalid json",
			input:    []byte(`not json`),
			expected: "",
			wantErr:  true,
		},
		{
			name:     "segments not array",
			input:    []byte(`[["not", "nested", "array"]]`),
			expected: "",
			wantErr:  true,
		},
		{
			name:     "unicode content",
			input:    []byte(`[[["こんにちは世界",null,"en",null,null,null,"gtx"]]]`),
			expected: "こんにちは世界",
			wantErr:  false,
		},
		{
			name:     "empty string in segment",
			input:    []byte(`[[["",null,"en",null,null,null,"gtx"]]]`),
			expected: "", // Empty string is valid JSON, gets added to parts, joins to empty string
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGoogleFreeResponse(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
