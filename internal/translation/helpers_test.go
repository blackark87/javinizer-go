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
// actressDisplayTitle tests
// =============================================================================

func TestActressDisplayTitle(t *testing.T) {
	tests := []struct {
		name     string
		actress  models.Actress
		expected string
	}{
		{
			name: "japanese name only",
			actress: models.Actress{
				JapaneseName: "田中香",
			},
			expected: "田中香",
		},
		{
			name: "first and last name",
			actress: models.Actress{
				FirstName: "Yui",
				LastName:  "Tanaka",
			},
			expected: "Tanaka Yui",
		},
		{
			name: "japanese name takes priority over first/last",
			actress: models.Actress{
				JapaneseName: "田中香",
				FirstName:    "Yui",
				LastName:     "Tanaka",
			},
			expected: "田中香",
		},
		{
			name:     "empty fields",
			actress:  models.Actress{},
			expected: "",
		},
		{
			name: "whitespace handling",
			actress: models.Actress{
				FirstName: "  Yui  ",
				LastName:  "  Tanaka  ",
			},
			expected: "Tanaka Yui",
		},
		{
			name: "only first name",
			actress: models.Actress{
				FirstName: "Yui",
			},
			expected: "Yui",
		},
		{
			name: "only last name",
			actress: models.Actress{
				LastName: "Tanaka",
			},
			expected: "Tanaka",
		},
		{
			name: "name with apostrophe",
			actress: models.Actress{
				FirstName: "Marie",
				LastName:  "O'Brien",
			},
			expected: "O'Brien Marie",
		},
		{
			name: "name with hyphen",
			actress: models.Actress{
				FirstName: "Anne",
				LastName:  "Smith-Jones",
			},
			expected: "Smith-Jones Anne",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actressDisplayTitle(tt.actress)
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
			name: "whitespace trimmed before splitting",
			actress: &models.Actress{},
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
			name: "full-width parenthesis stripped",
			actress: &models.Actress{},
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
			name: "Korean LLM output ignored - actress unchanged",
			actress: &models.Actress{
				JapaneseName: "まひる",
				FirstName:    "Mahiru",
				LastName:     "",
			},
			translated: "마히루",
			expected: models.Actress{
				JapaneseName: "まひる",
				FirstName:    "Mahiru",
				LastName:     "",
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
// parseStringArrayPayload tests
// =============================================================================

func TestParseStringArrayPayload(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "valid json array",
			input:    `["hello","world"]`,
			expected: []string{"hello", "world"},
			wantErr:  false,
		},
		{
			name:     "with code fences",
			input:    "```json[\"hello\",\"world\"]```",
			expected: []string{"hello", "world"},
			wantErr:  false,
		},
		{
			name:     "with code fences and newline",
			input:    "```json\n[\"hello\",\"world\"]\n```",
			expected: []string{"hello", "world"},
			wantErr:  false,
		},
		{
			name:     "with extra text before array",
			input:    "Here is the translation: [\"hello\"]",
			expected: []string{"hello"},
			wantErr:  false,
		},
		{
			name:     "with whitespace only",
			input:    "   ",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "invalid json",
			input:    "not a json array",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "malformed json string array",
			input:    `["Karen","She says "It's forceful..." but looks happy"]`,
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "empty strings in array",
			input:    `["hello","","world"]`,
			expected: []string{"hello", "", "world"},
			wantErr:  false,
		},
		{
			name:     "unicode characters",
			input:    `["こんにちは","世界"]`,
			expected: []string{"こんにちは", "世界"},
			wantErr:  false,
		},
		{
			name:     "escaped quotes in strings",
			input:    `["hello \"world\"","test"]`,
			expected: []string{"hello \"world\"", "test"},
			wantErr:  false,
		},
		{
			name:     "single element array",
			input:    `["single"]`,
			expected: []string{"single"},
			wantErr:  false,
		},
		{
			name:     "array with numbers coerced to strings",
			input:    `[1,2,3]`,
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStringArrayPayload(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
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
			name:          "standard format",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/miyosi_yuka.jpg",
			wantLastName:  "miyosi",
			wantFirstName: "yuka",
			wantOk:        true,
		},
		{
			name:          "trailing number stripped",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/miyasita_rena2.jpg",
			wantLastName:  "miyasita",
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
			name:     "single name part - not matched",
			thumbURL: "https://pics.dmm.co.jp/mono/actjpgs/yuka.jpg",
			wantOk:   false,
		},
		{
			name:          "query string ignored",
			thumbURL:      "https://pics.dmm.co.jp/mono/actjpgs/tanaka_yui.jpg?v=123",
			wantLastName:  "tanaka",
			wantFirstName: "yui",
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

		got, err := parseLLMTranslationPayload(input, 2)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"Karen",
			`She says "It's forceful..." but looks happy while being teased.`,
		}, got)
	})

	t.Run("falls back to json array payload", func(t *testing.T) {
		got, err := parseLLMTranslationPayload(`["hello","world"]`, 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"hello", "world"}, got)
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
