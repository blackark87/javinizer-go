package translation

import (
	"context"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateMovie_DMMReadingProtectsActressName(t *testing.T) {
	var inputs [][]string
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		inputs = append(inputs, append([]string(nil), texts...))
		return &translationResult{Texts: []string{"⟦0⟧의 유혹", "⟦0⟧가 등장하는 작품"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true, Description: true, Actresses: true},
	}, provider)
	movie := &models.Movie{
		Title:       "響蓮の誘惑",
		Description: "本編に響蓮が登場する",
		Actresses: []models.Actress{{
			JapaneseName: "響蓮",
			ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hibiki_ren.jpg",
		}},
	}

	output, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	assert.Empty(t, warning)
	require.Len(t, inputs, 1)
	assert.NotContains(t, strings.Join(inputs[0], "\n"), "響蓮")
	assert.Equal(t, "히비키 렌의 유혹", movie.Title)
	assert.Equal(t, "히비키 렌이 등장하는 작품", movie.Description)
	assert.Equal(t, "響蓮", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "히비키", movie.Actresses[0].LastName)
	assert.Equal(t, "렌", movie.Actresses[0].FirstName)
	require.NotNil(t, output)
	require.Len(t, output.Movie.Actresses, 1)
	assert.Equal(t, "히비키 렌", output.Movie.Actresses[0])
}

func TestBuildTranslationPlanPrefersProfileReadingOverOldThumbnailSlug(t *testing.T) {
	service := New(Config{Enabled: true, Fields: fieldsConfig{Actresses: true}})
	plan := service.BuildTranslationPlan(&models.Movie{Actresses: []models.Actress{{
		JapaneseName: "天然美月", Reading: "あまねみづき",
		ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/amane_kanon.jpg",
	}}}, "ko", "ja", "test")
	require.Len(t, plan.Fields, 1)
	assert.Equal(t, "actress", plan.Fields[0].FieldName)
	assert.Equal(t, "あまねみづき", plan.Fields[0].Text)
}

func TestKoreanJAVPromptTreatsMomoSiriAsBodyDescription(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "桃Siri")
	assert.Contains(t, rules, "애플힙")
	assert.Contains(t, rules, "never treated as a person name")
}

func TestKoreanJAVPromptUsesNaturalMiluchioAndVirilityTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "ミルチオ")
	assert.Contains(t, rules, "미루치오")
	assert.Contains(t, rules, "ミルチオの愛人 means a mistress who performs the technique")
	assert.Contains(t, rules, "미루치오를 해주는 불륜 상대")
	assert.Contains(t, rules, "never the nonsensical possessive 미루치오의 정부")
	assert.Contains(t, rules, "絶倫性欲者 referring to a woman → 절륜 색녀")
	assert.Contains(t, rules, "never 절륜 성욕자")
}

func TestKoreanJAVPromptDoesNotTransliterateShigoki(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "舐めシゴきフルコース → 혀와 손으로 뽑아주는 풀코스")
	assert.Contains(t, rules, "Never write 시고키")
	assert.Contains(t, rules, "must not mechanically become the corny phrase 사랑의")
}

func TestKoreanJAVPromptUsesNaturalEbisoriMassageWording(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "エビ反りオーガズム → 허리가 휘는 오르가슴")
	assert.Contains(t, rules, "Never write 허리 꺾인, 새우등처럼 휜")
	assert.NotContains(t, rules, "エビ反りオーガズム → 허리를 뒤로 젖히는 오르가슴")
	assert.Contains(t, rules, "特別施術 → 특별 코스")
}

func TestKoreanJAVPromptUsesYubunyeoInsteadOfIncheo(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "人妻 → 유부녀")
	assert.Contains(t, rules, "人妻もの → 유부녀물")
	assert.Contains(t, rules, "Never use the dated Japanese-calque term 인처")
}

func TestKoreanJAVPromptAvoidsJapaneseCalquesForSexFriendAndPrivateParts(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "セフレ志願の女の子 → 섹파를 자처하는 여자")
	assert.Contains(t, rules, "never transliterate it as 세프레")
	assert.Contains(t, rules, "ちんちんおっきしたら → 자지가 서면")
	assert.Contains(t, rules, "never the dictionary calque 비부")
	assert.Contains(t, rules, "きわどい秘部を触られすぎて → 은밀한 곳을 집요하게 만져져")
	assert.Contains(t, rules, "Do not mechanically transliterate 寝取られました as 네토라레 당했습니다")
}

func TestKoreanJAVPromptTreatsDoPrefixAsEmphasis(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "ド痴女 → 극강의 치녀")
	assert.Contains(t, rules, "never 도치녀")
	assert.Contains(t, rules, "結婚した妻 should normally be the concise 아내")
	assert.Contains(t, rules, "性欲おさまらない → 멈출 줄 모르는 성욕")
}

func TestKoreanJAVPromptTranslatesGyakuPakoByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "逆パコ is a woman initiating")
	assert.Contains(t, rules, "never transliterate them as 파코")
	assert.Contains(t, rules, "Never use the clinical 자궁경부")
	assert.Contains(t, rules, "Do not exaggerate it as 강타하다 or 집중 공략하다")
	assert.Contains(t, rules, "Never output 쥬보쥬보")
	assert.Contains(t, rules, "Never transliterate Japanese sexual sound-symbolic words into Hangul")
	assert.Contains(t, rules, "never the food term 자지 샤브샤브")
	assert.Contains(t, rules, "おしゃぶり means 펠라 or 자지 빨기")
	assert.Contains(t, rules, "never use the awkward 한 판 박아버리다")
	assert.Contains(t, rules, "never transliterate it as 역파코")
	assert.Contains(t, rules, "がっつり痴女られたい → 치녀에게 실컷 농락당하고 싶다")
	assert.Contains(t, rules, "never 듬뿍 치녀 취급당하고 싶다")
}

func TestKoreanJAVPromptTranslatesTetsumanAsExplicitSlang(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "鉄マン is explicit JAV slang")
	assert.Contains(t, rules, "translate it as 강철 보지")
	assert.Contains(t, rules, "never transliterate it as 철맨")
	assert.Contains(t, rules, "秘技教本 → 비법 교본")
	assert.Contains(t, rules, "never 생하메, 생삽입")
}

func TestBuildLLMQualityReviewPromptIncludesSourceCandidateAndStrictOutput(t *testing.T) {
	items := []qualityReviewItem{{Source: "鉄マン", Candidate: "철맨"}}
	systemPrompt, userPrompt, err := buildLLMQualityReviewPromptsWithMarkers("ko", items, []string{"<<<quality_review_title>>>"})
	require.NoError(t, err)
	assert.Contains(t, systemPrompt, "mandatory second-pass quality reviewer")
	assert.Contains(t, systemPrompt, "鉄マン")
	assert.Contains(t, userPrompt, "[JAPANESE SOURCE]\n鉄マン")
	assert.Contains(t, userPrompt, "[KOREAN CANDIDATE]\n철맨")
	assert.Contains(t, userPrompt, "<<<quality_review_title>>>")
	assert.NotContains(t, userPrompt, "[corrected Korean]")
}

func TestSanitizeQualityReviewTextRemovesEchoedOutputLabel(t *testing.T) {
	assert.Equal(t, "강철 보지", sanitizeQualityReviewText("[corrected Korean]\n강철 보지"))
	assert.Equal(t, "강철 보지", sanitizeQualityReviewText("강철 보지"))
}

func TestTranslateTextsSplitsCombinedRequestAfterGemmaParserError(t *testing.T) {
	calls := make([]int, 0)
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls = append(calls, len(texts))
		if len(texts) > 1 {
			return nil, &translationError{Kind: TranslationErrorProvider, Message: "peg-gemma4 format"}
		}
		return &translationResult{Texts: []string{"분리 성공"}}, nil
	}}
	service := New(Config{Provider: "mock"}, provider)

	result, err := service.translateTexts(context.Background(), "ja", "ko", []string{"제목", "설명"}, []string{"title", "description"})

	require.NoError(t, err)
	assert.Equal(t, []string{"분리 성공", "분리 성공"}, result)
	assert.Equal(t, []int{2, 1, 1}, calls)
}

type splittingQualityReviewProvider struct {
	calls []int
}

func (p *splittingQualityReviewProvider) Name() string { return "openai-compatible" }

func (p *splittingQualityReviewProvider) Translate(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
	p.calls = append(p.calls, len(texts))
	if len(texts) > 1 {
		return nil, &translationError{Kind: TranslationErrorProvider, Message: "peg-gemma4 format"}
	}
	return &translationResult{Texts: []string{"검수 성공"}}, nil
}

func TestReviewJAVTranslationsSplitsAfterGemmaParserError(t *testing.T) {
	provider := &splittingQualityReviewProvider{}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{
		{FieldName: "quality_review_title", Source: "原題", Candidate: "후보 제목"},
		{FieldName: "quality_review_description", Source: "説明", Candidate: "후보 설명"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"검수 성공", "검수 성공"}, result)
	assert.Equal(t, []int{2, 1, 1}, provider.calls)
}

type qualityReviewMockProvider struct {
	items []qualityReviewItem
}

func (p *qualityReviewMockProvider) Name() string { return "openai-compatible" }

func (p *qualityReviewMockProvider) Translate(ctx context.Context, _, _ string, texts []string) (*translationResult, error) {
	p.items, _ = qualityReviewFromContext(ctx, len(texts))
	return &translationResult{Texts: []string{"강철 보지"}}, nil
}

func TestReviewJAVTranslationsPassesOriginalAndCandidateToSecondPass(t *testing.T) {
	provider := &qualityReviewMockProvider{}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title", Source: "鉄マン", Candidate: "철맨",
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"강철 보지"}, result)
	require.Len(t, provider.items, 1)
	assert.Equal(t, "鉄マン", provider.items[0].Source)
	assert.Equal(t, "철맨", provider.items[0].Candidate)
}

func TestTranslateMovie_RetriesNonHangulPersonSlot(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls++
		if len(texts) == 2 {
			return &translationResult{Texts: []string{"멋진 작품", "Hibiki Ren"}}, nil
		}
		return &translationResult{Texts: []string{"히비키 렌"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true, Actresses: true},
	}, provider)
	movie := &models.Movie{Title: "素敵な作品", Actresses: []models.Actress{{JapaneseName: "響蓮"}}}

	output, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	assert.Empty(t, warning)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "히비키 렌", output.Movie.Actresses[0])
	assert.Equal(t, "響蓮", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "히비키", movie.Actresses[0].LastName)
	assert.Equal(t, "렌", movie.Actresses[0].FirstName)
}

func TestTranslateMovie_RetriesResidualJapaneseSlot(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
		calls++
		if calls == 1 {
			return &translationResult{Texts: []string{"격차가 최고すぎる"}}, nil
		}
		return &translationResult{Texts: []string{"격차가 너무 좋다"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true},
	}, provider)
	movie := &models.Movie{Title: "格差が最高すぎる"}

	_, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	assert.Empty(t, warning)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "격차가 너무 좋다", movie.Title)
}

func TestTranslateTexts_FallsBackOnMergedSlotAnomaly(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls++
		if len(texts) == 2 {
			return &translationResult{Texts: []string{strings.Repeat("merged ", 30), "second"}}, nil
		}
		return &translationResult{Texts: []string{"single"}}, nil
	}}
	service := New(Config{Provider: "mock"}, provider)

	translated, err := service.translateTexts(context.Background(), "ja", "ko", []string{"短い", "説明"}, []string{"title", "description"})
	require.NoError(t, err)
	assert.Equal(t, []string{"single", "single"}, translated)
	assert.Equal(t, 3, calls)
}
