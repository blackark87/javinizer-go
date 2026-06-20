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
			name: "japanese actress - preserves JapaneseName, splits GivenName FamilyName",
			actress: &models.Actress{
				JapaneseName: "田中香",
			},
			translated: "Yui Tanaka",
			expected: models.Actress{
				JapaneseName: "田中香",
				FirstName:    "Yui",
				LastName:     "Tanaka",
			},
		},
		{
			name: "actress with existing english name - splits translated value",
			actress: &models.Actress{
				FirstName: "Yui",
				LastName:  "Tanaka",
			},
			translated: "Yui Tanaka",
			expected: models.Actress{
				JapaneseName: "",
				FirstName:    "Yui",
				LastName:     "Tanaka",
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
			name: "three word name - first word is FirstName, rest is LastName",
			actress: &models.Actress{
				JapaneseName: "田中",
			},
			translated: "Maria De Niro",
			expected: models.Actress{
				JapaneseName: "田中",
				FirstName:    "Maria",
				LastName:     "De Niro",
			},
		},
		{
			name: "whitespace trimmed before splitting",
			actress: &models.Actress{},
			translated: "  Yui Tanaka  ",
			expected: models.Actress{
				FirstName: "Yui",
				LastName:  "Tanaka",
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
