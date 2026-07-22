package translation

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTranslationPlanProtectsJapaneseActressNameWhitespaceVariants(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Description: true}})
	for _, sourceName := range []string{"野々浦暖", "野々浦 暖", "野々浦　暖"} {
		t.Run(sourceName, func(t *testing.T) {
			movie := &models.Movie{
				Description: sourceName + "が出演する作品",
				Actresses: []models.Actress{{
					JapaneseName: "野々浦暖",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/nonoura_non.jpg",
				}},
			}

			plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
			require.Len(t, plan.Fields, 1)
			field := plan.Fields[0]
			assert.Equal(t, "description", field.FieldName)
			assert.NotContains(t, field.Text, "野々浦")
			assert.Contains(t, field.Text, "⟦0⟧")
			assert.Equal(t, "노노우라 논", field.Placeholders["⟦0⟧"])
			assert.Contains(t, field.FallbackText, "노노우라 논")

			restored, ok := restoreNamePlaceholders(field.Text, field.Placeholders)
			require.True(t, ok)
			assert.Equal(t, "노노우라 논が出演する作品", restored)
		})
	}
}

func TestBuildTranslationPlanProtectsLongestOverlappingActressNameFirst(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Description: true}})
	movie := &models.Movie{
		Description: "野々浦 暖が出演する作品",
		Actresses: []models.Actress{
			{JapaneseName: "野々浦", FirstName: "노노우라"},
			{JapaneseName: "野々浦暖", FirstName: "논", LastName: "노노우라"},
		},
	}

	plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
	require.Len(t, plan.Fields, 1)
	field := plan.Fields[0]
	assert.NotContains(t, field.Text, "暖", "the shorter overlapping name must not consume the prefix")
	require.Len(t, field.Placeholders, 1)
	for token, name := range field.Placeholders {
		assert.True(t, strings.Contains(field.Text, token))
		assert.Equal(t, "노노우라 논", name)
	}
}

func TestBuildTranslationPlanProtectsJapaneseActressNameInTitle(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Title: true}})
	movie := &models.Movie{
		Title: "野々浦 暖の作品",
		Actresses: []models.Actress{{
			JapaneseName: "野々浦暖",
			ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/nonoura_non.jpg",
		}},
	}

	plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
	require.Len(t, plan.Fields, 1)
	field := plan.Fields[0]
	assert.Equal(t, "title", field.FieldName)
	assert.NotContains(t, field.Text, "野々浦")
	assert.Contains(t, field.Text, "⟦0⟧")
	assert.Equal(t, "노노우라 논", field.Placeholders["⟦0⟧"])
}

func TestDMMActressNamesPreserveFamilyAndGivenNameFields(t *testing.T) {
	tests := []struct {
		name         string
		japaneseName string
		thumbURL     string
		wantLast     string
		wantFirst    string
	}{
		{
			name:         "sena nanami",
			japaneseName: "星七ななみ",
			thumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/sena_nanami.jpg",
			wantLast:     "세나", wantFirst: "나나미",
		},
		{
			name:         "shiraiwa tomo from nihon-shiki slug",
			japaneseName: "白岩冬萌",
			thumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/siraiwa_tomo.jpg",
			wantLast:     "시라이와", wantFirst: "토모",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			last, first, ok := extractNamesFromDMMActjpgsURL(tt.thumbURL)
			require.True(t, ok)
			hangul, ok := romajiToHangul(joinRomanizedName(last, first))
			require.True(t, ok)
			assert.Equal(t, tt.wantLast+" "+tt.wantFirst, hangul)
			cleaned := models.Actress{JapaneseName: tt.japaneseName, ThumbURL: tt.thumbURL}
			CleanStoredActress(&cleaned)
			assert.Equal(t, tt.japaneseName, cleaned.JapaneseName)
			pattern := flexibleJapaneseNamePattern(tt.japaneseName)
			require.NotNil(t, pattern)
			assert.True(t, pattern.MatchString("素敵な作品 "+tt.japaneseName))

			service := New(Config{Fields: fieldsConfig{Title: true, Actresses: true}})
			movie := &models.Movie{
				Title:     "素敵な作品 " + tt.japaneseName,
				Actresses: []models.Actress{{JapaneseName: tt.japaneseName, ThumbURL: tt.thumbURL}},
			}
			plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
			require.Len(t, plan.Fields, 2)

			titleField := plan.Fields[0]
			assert.NotContains(t, titleField.Text, tt.japaneseName)
			require.Len(t, titleField.Placeholders, 1)
			wantDisplay := tt.wantLast + " " + tt.wantFirst
			assert.Contains(t, titleField.FallbackText, wantDisplay)

			actressField := plan.Fields[1]
			require.NotNil(t, actressField.Preset)
			assert.Equal(t, wantDisplay, *actressField.Preset)
			translated := movie.Actresses[0]
			replaceActressName(&translated, *actressField.Preset)
			assert.Equal(t, tt.wantLast, translated.LastName)
			assert.Equal(t, tt.wantFirst, translated.FirstName)
			assert.Equal(t, tt.japaneseName, translated.JapaneseName)
		})
	}
}

func TestBuildTranslationPlanProtectsRepeatedGivenNameWithHonorific(t *testing.T) {
	service := New(Config{Fields: fieldsConfig{Title: true, Actresses: true}})
	movie := &models.Movie{
		Title: "気持ちいいと声が大きくなる七緒ちゃん 彩月七緒",
		Actresses: []models.Actress{{
			JapaneseName: "彩月七緒",
			ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/satuki_nao.jpg",
		}},
	}

	plan := service.BuildTranslationPlan(movie, "ko", "ja", "test")
	require.Len(t, plan.Fields, 2)
	title := plan.Fields[0]
	assert.NotContains(t, title.Text, "七緒ちゃん")
	assert.NotContains(t, title.Text, "彩月七緒")
	assert.Contains(t, title.FallbackText, "나오짱")
	assert.Contains(t, title.FallbackText, "사츠키 나오")
	assert.NotContains(t, title.FallbackText, "나나짱")
}
