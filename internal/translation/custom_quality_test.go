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

func TestKoreanJAVPromptTranslatesRemoteVibeAndFawnClimaxByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "寸止めリモバイ調教 → 리모트 바이브로 절정 직전까지 애태우는 조교")
	assert.Contains(t, rules, "must be rendered as 리모트 바이브")
	assert.Contains(t, rules, "never transliterated as 리모바이")
	assert.Contains(t, rules, "膝ガクガク小鹿アクメ → 무릎이 후들거리는 절정")
	assert.Contains(t, rules, "never 새끼 사슴 오르가슴")
}

func TestKoreanJAVPromptTreatsDoPrefixAsEmphasis(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "ド痴女 → 극강의 치녀")
	assert.Contains(t, rules, "never 도치녀")
	assert.Contains(t, rules, "結婚した妻 should normally be the concise 아내")
	assert.Contains(t, rules, "性欲おさまらない → 멈출 줄 모르는 성욕")
}

func TestKoreanJAVPromptTranslatesIinariDoMByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "言いなり and イイナリ")
	assert.Contains(t, rules, "never transliterate them as 이이나리")
	assert.Contains(t, rules, "ドM means an extreme masochist")
	assert.Contains(t, rules, "never 도M")
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

func TestKoreanJAVPromptTranslatesOrificeSwallowingAndSquirtingCompounds(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "3穴 → 3홀 or 세 구멍")
	assert.Contains(t, rules, "2穴セフレ → 2홀 섹파 or 두 구멍을 내주는 섹파")
	assert.Contains(t, rules, "never the Hanja reading 혈")
	assert.Contains(t, rules, "never transliterate it as 고쿤")
	assert.Contains(t, rules, "ケツマンコ means 후장")
	assert.Contains(t, rules, "ストゼロ is the alcoholic drink brand Strong Zero")
	assert.Contains(t, rules, "never 스트로제로 or 스트로 제로")
	assert.Contains(t, rules, "潮吹き means squirting sexual fluid")
	assert.Contains(t, rules, "限界ストゼロ潮吹きFUCK → 스트롱 제로를 마시며 한계까지 분수를 뿜는 섹스")
}

func TestKoreanJAVPromptTranslatesAmateurHostessPoseAndNTRTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "never transliterate them as 시로토 or 시로트")
	assert.Contains(t, rules, "逆ナンパ has the same female-pickup meaning")
	assert.Contains(t, rules, "キャバ嬢 means a hostess")
	assert.Contains(t, rules, "never transliterate it as 캬바죠 or 카바죠")
	assert.Contains(t, rules, "スタイル最強 and 最強スタイル → 최강 몸매")
	assert.Contains(t, rules, "理想のモテ体型 and 理想のモテ体系 → 이상적인 인기 몸매")
	assert.Contains(t, rules, "極エロ → 극도로 야한 or 극강의 야함")
	assert.Contains(t, rules, "寝取り is the act of taking or seducing someone else's partner")
	assert.Contains(t, rules, "ちんぐり返し depicts a man on his back")
	assert.Contains(t, rules, "ちんぐり返し騎乗位 → 남자의 다리를 뒤로 젖힌 기승위")
	assert.Contains(t, rules, "Do not name the pose or add domination and violence absent from the source")
	assert.Contains(t, rules, "never use shape metaphors such as 새우, 쟁기, or 활")
	assert.Contains(t, rules, "タイマン4本番 → 1대1 맞대결 본방 4회")
	assert.Contains(t, rules, "胸糞NTR → 역겨운 NTR or 기분 더러운 NTR")
	assert.Contains(t, rules, "never 울울한 발기")
}

func TestKoreanJAVPromptTranslatesExplicitMetaphorsAndKeywordProperNouns(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "蜜壺 is a metaphor for the vagina")
	assert.Contains(t, rules, "never use the dictionary calques 밀통, 꿀단지, or 비부")
	assert.Contains(t, rules, "手マン performed by another person means 핑거링")
	assert.Contains(t, rules, "浅草 is the place name 아사쿠사")
	assert.Contains(t, rules, "ordinary fortune-result 大吉 means 대길 or 대박")
	assert.Contains(t, rules, "玩具責め in promotional headlines means 성인용품 공세")
	assert.Contains(t, rules, "確定ビッチ describes an unmistakably promiscuous woman")
	assert.Contains(t, rules, "大・連・発 or 大連発 describes a woman's climax")
	assert.Contains(t, rules, "ヤリモクインフルエンサー → 섹스만 노리는 인플루언서")
	assert.Contains(t, rules, "完全主観 means 완전 1인칭 시점")
	assert.Contains(t, rules, "青春グラフィティ means 청춘 이야기 or 청춘 기록")
	assert.Contains(t, rules, "【...】 must remain 【...】")
}

func TestTranslationPromptForbidsSubstitutingPerformerNames(t *testing.T) {
	systemPrompt, _, err := buildLLMTranslationPromptsWithMarkers("ja", "ko", []string{"作品名 晶エリー"}, []string{"<<<title>>>"})
	require.NoError(t, err)
	assert.Contains(t, systemPrompt, "Never invent, anglicize, or substitute a different performer name")
}

func TestKoreanJAVPromptTranslatesNewContextualSlangByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "吸引おしゃぶり → 빨아들이는 펠라")
	assert.Contains(t, rules, "Never output 키메섹")
	assert.Contains(t, rules, "キメセクの巣 → 약물 섹스의 소굴")
	assert.Contains(t, rules, "タイパを気にし過ぎる → 시간 효율을 지나치게 따지는")
	assert.Contains(t, rules, "never 페더 손가락 핸드잡")
	assert.Contains(t, rules, "never write 러브호텔 물바다")
	assert.Contains(t, rules, "never transliterate it as 야리만")
	assert.Contains(t, rules, "逆ナンドライブ → 남자를 헌팅하는 드라이브")
	assert.Contains(t, rules, "never default to the stiff legal calque 음행")
	assert.Contains(t, rules, "never 말뚝박기 피스톤")
}

func TestKoreanJAVPromptTranslatesTetsumanAsExplicitSlang(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "鉄マン is explicit JAV slang")
	assert.Contains(t, rules, "translate it as 강철 보지")
	assert.Contains(t, rules, "never transliterate it as 철맨")
	assert.Contains(t, rules, "秘技教本 → 비법 교본")
	assert.Contains(t, rules, "never 생하메, 생삽입")
}

func TestKoreanJAVPromptCoversNewMissTranslationTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	for _, expected := range []string{
		"never use the literal 늪",
		"枕営業 means 성상납",
		"never the dictionary calque 색백",
		"僕の身代わりに means 나 대신",
		"never transliterate it as 바쿠누키",
		"挟射 means ejaculation while held between the breasts",
		"おま○こよわよわ → 보지 허접",
	} {
		assert.Contains(t, rules, expected)
	}
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

func TestSanitizeQualityReviewTextExtractsFinalTextAfterGemmaPromptEcho(t *testing.T) {
	candidate := "청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 ⟦7000⟧"
	echoed := "[JAPANESE SOURCE]\nアオハル 制服美少女 160分 ⟦7000⟧\n" +
		"[KOREAN CANDIDATE]\n" + candidate + "\n\n" +
		"청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 ⟦7000⟧"

	assert.Equal(t,
		"청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 ⟦7000⟧",
		sanitizeQualityReviewTextWithCandidate(echoed, candidate),
	)
	assert.Equal(t,
		candidate+"\n\n청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 ⟦7000⟧",
		sanitizeQualityReviewTextWithCandidate(echoed, "다른 후보"),
	)
}

func TestSanitizeQualityReviewTextUsesEchoedKoreanCandidate(t *testing.T) {
	original := "과격한 몰래카메라 枕営業 로케 ⟦7000⟧"
	echoed := "[JAPANESE SOURCE]\n過激ドッキリ枕営業ロケ ⟦7000⟧\n" +
		"[KOREAN CANDIDATE]\n과격한 몰래카메라 성상납 촬영 ⟦7000⟧"
	assert.Equal(t, "과격한 몰래카메라 성상납 촬영 ⟦7000⟧",
		sanitizeQualityReviewTextWithCandidate(echoed, original))
}

func TestSanitizeQualityReviewTextPrefersCandidateOverMalformedTrailingBlock(t *testing.T) {
	candidate := "농밀한 교감, 하얀 피부의 거유 미소녀 ⟦7000⟧"
	echoed := "[JAPANESE SOURCE]\n濃交 色白美少女 ⟦7000⟧\n" +
		"[KOREAN CANDIDATE]\n" + candidate + "\n\n濃交 하얀 피부의 미소녀 ⟦7000⟧"
	assert.Equal(t, candidate, sanitizeQualityReviewTextWithCandidate(echoed, candidate))
}

func TestInvalidQualityReviewTextRejectsPromptEchoAndResidualJapanese(t *testing.T) {
	assert.True(t, isInvalidQualityReviewText("[JAPANESE SOURCE]\n原題\n[KOREAN CANDIDATE]\n후보"))
	assert.True(t, isInvalidQualityReviewText("한국어 原題"))
	assert.True(t, isInvalidQualityReviewText(""))
	assert.False(t, isInvalidQualityReviewText("자연스런 한국어 검수 결과"))
}

type promptEchoQualityReviewProvider struct{}

func (p *promptEchoQualityReviewProvider) Name() string { return "openai-compatible" }

func (p *promptEchoQualityReviewProvider) Translate(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
	return &translationResult{Texts: []string{"[JAPANESE SOURCE]\n原題\n[KOREAN CANDIDATE]\n정상 후보"}}, nil
}

func TestReviewJAVTranslationsAcceptsEchoedKoreanCandidateSlot(t *testing.T) {
	provider := &promptEchoQualityReviewProvider{}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title", Source: "原題", Candidate: "정상 후보",
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"정상 후보"}, result)
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
	items    []qualityReviewItem
	response string
}

func (p *qualityReviewMockProvider) Name() string { return "openai-compatible" }

func (p *qualityReviewMockProvider) Translate(ctx context.Context, _, _ string, texts []string) (*translationResult, error) {
	p.items, _ = qualityReviewFromContext(ctx, len(texts))
	response := p.response
	if response == "" {
		response = "강철 보지"
	}
	return &translationResult{Texts: []string{response}}, nil
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

func TestReviewJAVTranslationsAcceptsFinalTextAfterGemmaPromptEcho(t *testing.T) {
	candidate := "청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 ⟦7000⟧"
	provider := &qualityReviewMockProvider{response: "[JAPANESE SOURCE]\nアオハル 制服美少女 160分 ⟦7000⟧\n" +
		"[KOREAN CANDIDATE]\n" + candidate + "\n\n" +
		"청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 ⟦7000⟧"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "アオハル 制服美少女 160分 流川夕",
		Candidate: "청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 루카와 유",
		Actresses: []models.Actress{{JapaneseName: "流川夕", LastName: "루카와", FirstName: "유"}},
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"청춘 교복 미소녀와 보내는 성춘 3SEX. 160분 루카와 유"}, result)
}

func TestReviewJAVTranslationsProtectsActressNames(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "더 자연스러운 제목 ⟦7000⟧"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "素敵な作品 松本いちか",
		Candidate: "멋진 작품 마츠모토 이치카",
		Actresses: []models.Actress{{JapaneseName: "松本いちか", LastName: "마츠모토", FirstName: "이치카"}},
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"더 자연스러운 제목 마츠모토 이치카"}, result)
	require.Len(t, provider.items, 1)
	assert.Equal(t, "素敵な作品 ⟦7000⟧", provider.items[0].Source)
	assert.Equal(t, "멋진 작품 ⟦7000⟧", provider.items[0].Candidate)
}

func TestReviewJAVTranslationsRejectsDroppedActressNameToken(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "배우 이름이 사라진 제목"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	_, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "素敵な作品 松本いちか",
		Candidate: "멋진 작품 마츠모토 이치카",
		Actresses: []models.Actress{{JapaneseName: "松本いちか", LastName: "마츠모토", FirstName: "이치카"}},
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dropped a protected performer name")
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
