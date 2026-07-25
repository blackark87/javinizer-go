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
	assert.Contains(t, rules, "桃尻/桃Siri→애플힙")
	assert.Contains(t, rules, "never treat as a person name")
}

func TestKoreanJAVPromptUsesNaturalMiluchioAndVirilityTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "ミルチオ→미루치오")
	assert.Contains(t, rules, "ミルチオの愛人→미루치오를 해주는 불륜 상대")
	assert.Contains(t, rules, "금지: 미루치오의 정부")
	assert.Contains(t, rules, "female 絶倫性欲者→절륜 색녀")
	assert.Contains(t, rules, "금지: 절륜 성욕자")
}

func TestKoreanJAVPromptDoesNotTransliterateShigoki(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "舐めシゴきフルコース→혀와 손으로 뽑아주는 풀코스")
	assert.Contains(t, rules, "금지: 시고키/핥기 시고키")
	assert.Contains(t, rules, "사랑의로 기계 번역하지 않는다")
}

func TestKoreanJAVPromptUsesNaturalEbisoriMassageWording(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "エビ反りオーガズム→허리가 휘는 오르가슴")
	assert.Contains(t, rules, "금지: 허리 꺾인/새우등처럼 휜")
	assert.NotContains(t, rules, "エビ反りオーガズム→허리를 뒤로 젖히는 오르가슴")
	assert.Contains(t, rules, "特別施術→특별 코스")
}

func TestKoreanJAVPromptUsesYubunyeoInsteadOfIncheo(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "人妻→유부녀")
	assert.Contains(t, rules, "人妻もの→유부녀물")
	assert.Contains(t, rules, "금지: 인처")
}

func TestKoreanJAVPromptAvoidsJapaneseCalquesForSexFriendAndPrivateParts(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "セフレ志願の女の子→섹파를 자처하는 여자")
	assert.Contains(t, rules, "금지: 세프레")
	assert.Contains(t, rules, "ちんちんおっきしたら→자지가 서면")
	assert.Contains(t, rules, "금지: 비부")
	assert.Contains(t, rules, "きわどい秘部を触られすぎて→은밀한 곳을 집요하게 만져져")
	assert.Contains(t, rules, "寝取られました→다른 남자에게 넘어가 버렸다")
	assert.Contains(t, rules, "네토라레 당했습니다로 음차하지 않는다")
}

func TestKoreanJAVPromptTranslatesRemoteVibeAndFawnClimaxByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "寸止めリモバイ調教→리모트 바이브로 절정 직전까지 애태우는 조교")
	assert.Contains(t, rules, "リモバイ→리모트 바이브")
	assert.Contains(t, rules, "금지: 리모바이")
	assert.Contains(t, rules, "膝ガクガク小鹿アクメ→무릎이 후들거리는 절정")
	assert.Contains(t, rules, "금지: 새끼 사슴 오르가슴")
}

func TestKoreanJAVPromptTreatsDoPrefixAsEmphasis(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "ド痴女→극강의 치녀")
	assert.Contains(t, rules, "금지: 도치녀")
	assert.Contains(t, rules, "結婚した妻→아내")
	assert.Contains(t, rules, "性欲おさまらない→멈출 줄 모르는 성욕")
}

func TestKoreanJAVPromptTranslatesIinariDoMByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "言いなり/イイナリ→시키는 대로 하는")
	assert.Contains(t, rules, "금지: 이이나리")
	assert.Contains(t, rules, "ドM→극M")
	assert.Contains(t, rules, "금지: 도M")
}

func TestKoreanJAVPromptTranslatesGyakuPakoByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "逆パコ→여자가 덮치는")
	assert.Contains(t, rules, "パコ/パコる/パコパコ→섹스/섹스하다/박아대다")
	assert.Contains(t, rules, "금지: 강타하다/집중 공략하다/자궁경부")
	assert.Contains(t, rules, "금지: 쥬보쥬보")
	assert.Contains(t, rules, "Japanese sexual sounds must describe action/result")
	assert.Contains(t, rules, "금지: 자지 샤브샤브")
	assert.Contains(t, rules, "sexual おしゃぶり→펠라|자지 빨기")
	assert.Contains(t, rules, "금지: 한 판 박아버리다")
	assert.Contains(t, rules, "금지: 역파코")
	assert.Contains(t, rules, "がっつり痴女られたい→치녀에게 실컷 농락당하고 싶다")
	assert.Contains(t, rules, "금지: 듬뿍 치녀 취급당하고 싶다")
}

func TestKoreanJAVPromptTranslatesOrificeSwallowingAndSquirtingCompounds(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "3穴→3홀|세 구멍")
	assert.Contains(t, rules, "2穴セフレ→2홀 섹파|두 구멍을 내주는 섹파")
	assert.Contains(t, rules, "금지: 혈/untranslated 穴")
	assert.Contains(t, rules, "금지: 고쿤")
	assert.Contains(t, rules, "ケツマンコ→후장")
	assert.Contains(t, rules, "ストゼロ is Strong Zero→스트롱 제로")
	assert.Contains(t, rules, "금지: 스트로제로/스트로 제로")
	assert.Contains(t, rules, "潮吹き→분수|애액 분출|애액을 뿜다")
	assert.Contains(t, rules, "限界ストゼロ潮吹きFUCK→스트롱 제로를 마시며 한계까지 분수를 뿜는 섹스")
}

func TestKoreanJAVPromptTranslatesAmateurHostessPoseAndNTRTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "금지: 시로토/시로트")
	assert.Contains(t, rules, "逆ナン/逆ナンパ→여자가 남자를 헌팅하는")
	assert.Contains(t, rules, "キャバ嬢→캬바걸|캬바클럽 호스티스")
	assert.Contains(t, rules, "금지: 캬바죠/카바죠")
	assert.Contains(t, rules, "スタイル最強/最強スタイル→최강 몸매")
	assert.Contains(t, rules, "理想のモテ体型/理想のモテ体系→이상적인 인기 몸매")
	assert.Contains(t, rules, "極エロ→극도로 야한|극강의 야함")
	assert.Contains(t, rules, "寝取り=남이 가진 파트너를 빼앗음")
	assert.Contains(t, rules, "ちんぐり返し는 남자를 눕혀")
	assert.Contains(t, rules, "ちんぐり返し騎乗位→남자의 다리를 뒤로 젖힌 기승위")
	assert.Contains(t, rules, "지배/폭력을 추가하지 않는다")
	assert.Contains(t, rules, "금지: 친구리/친구리카에시/치무가에리/새우/쟁기/활 비유")
	assert.Contains(t, rules, "タイマン4本番→1대1 맞대결 본방 4회")
	assert.Contains(t, rules, "胸糞NTR→역겨운 NTR|기분 더러운 NTR")
	assert.Contains(t, rules, "금지: 울울한 발기")
}

func TestKoreanJAVPromptTranslatesExplicitMetaphorsAndKeywordProperNouns(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "蜜壺→보지|질")
	assert.Contains(t, rules, "금지: 밀통/꿀단지/비부")
	assert.Contains(t, rules, "another-person 手マン→핑거링")
	assert.Contains(t, rules, "浅草→아사쿠사")
	assert.Contains(t, rules, "ordinary fortune 大吉→대길|대박")
	assert.Contains(t, rules, "玩具責め→성인용품 공세|장난감 조교")
	assert.Contains(t, rules, "確定ビッチ→확실한 문란녀")
	assert.Contains(t, rules, "female-climax 大・連・発/大連発→연속 절정")
	assert.Contains(t, rules, "ヤリモクインフルエンサー→섹스만 노리는 인플루언서")
	assert.Contains(t, rules, "完全主観→완전 1인칭 시점")
	assert.Contains(t, rules, "青春グラフィティ→청춘 이야기|청춘 기록")
	assert.Contains(t, rules, "【...】 stays 【...】")
}

func TestTranslationPromptForbidsSubstitutingPerformerNames(t *testing.T) {
	systemPrompt, _, err := buildLLMTranslationPromptsWithMarkers("ja", "ko", []string{"作品名 晶エリー"}, []string{"<<<title>>>"})
	require.NoError(t, err)
	assert.Contains(t, systemPrompt, "Never invent, anglicize, or substitute a different performer name")
}

func TestBuildLLMTranslationPrompts_CoversLatestMissTranslationCases(t *testing.T) {
	texts := []string{
		"メンズエステで中出しまでさせてくれる痴女お姉さんはガチ恋営業chu 斎藤あみり",
		"ピンク髪のギャルJ系に監禁されて、ざこざこざぁ～こと罵られて大人のプライドを打ち砕かれて逆レ搾精されまくった 斎藤あみり",
		"佐倉絆 初アナル解禁",
	}
	systemPrompt, userPrompt, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		texts,
		[]string{"<<<title[0]>>>", "<<<title[1]>>>", "<<<title[2]>>>"},
	)
	require.NoError(t, err)
	for _, source := range texts {
		assert.Contains(t, userPrompt, source)
	}

	for _, expected := range []string{
		"HIGHEST PRIORITY exact form",
		"Latin suffix chu is a kiss sound",
		"ガチ恋営業chu→진심인 척하는 영업 츄",
		"금지: 영업 중/가치코이",
		"逆レ/逆レイプ→역강간",
		"逆レ搾精→역강간 착정",
		"source에 パコ가 없으면 역파코/파코를 넣지 않는다",
		"アナル→애널 for JAV act/genre",
		"初アナル解禁→첫 애널 해금",
	} {
		assert.Contains(t, systemPrompt, expected)
	}
	assert.NotContains(t, systemPrompt, "ちんぐり返しアナル舐め→남자의 다리를 뒤로 젖혀 항문 핥기")
	assert.Contains(t, systemPrompt, "鉄マン→강철 보지")
	assert.NotContains(t, userPrompt, "[translation]")
	for _, marker := range []string{"<<<title[0]>>>", "<<<title[1]>>>", "<<<title[2]>>>"} {
		assert.Equal(t, 1, strings.Count(userPrompt, marker))
	}
}

func TestKoreanJAVPromptTranslatesNewContextualSlangByMeaning(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "吸引おしゃぶり→빨아들이는 펠라")
	assert.Contains(t, rules, "금지: 키메섹/킴세쿠/키메세쿠")
	assert.Contains(t, rules, "キメセクの巣→약물 섹스의 소굴")
	assert.Contains(t, rules, "タイパを気にし過ぎる→시간 효율을 지나치게 따지는")
	assert.Contains(t, rules, "금지: 페더 손가락 핸드잡")
	assert.Contains(t, rules, "금지: 러브호텔 물바다")
	assert.Contains(t, rules, "금지: 야리만")
	assert.Contains(t, rules, "逆ナンドライブ→남자를 헌팅하는 드라이브")
	assert.Contains(t, rules, "use 음행 only for clear legal misconduct")
	assert.Contains(t, rules, "금지: 말뚝박기 피스톤")
}

func TestKoreanJAVPromptTranslatesTetsumanAsExplicitSlang(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "鉄マン→강철 보지")
	assert.Contains(t, rules, "금지: 철맨")
	assert.Contains(t, rules, "秘技教本→비법 교본")
	assert.Contains(t, rules, "금지: 생하메/생삽입")
}

func TestKoreanJAVPromptCoversNewMissTranslationTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	for _, expected := range []string{
		"slang-suffix 沼→푹 빠지는|헤어나올 수 없는",
		"금지: 편리한/조건 좋은",
		"枕営業→성상납",
		"금지: 농교",
		"금지: 색백",
		"금지: 미거유",
		"エロかわ/エロ可愛い→야하고 귀여운",
		"乳首エステ→유두 마사지",
		"舐めテク/ハンドテク→혀 테크닉/손 테크닉",
		"僕の身代わりに→나 대신",
		"금지: 바쿠누키",
		"挟射→가슴 사이에 끼워 사정",
		"おま○こよわよわ→보지 허접",
		"マンスジ→보지 윤곽",
		"title 食い込みマンスジ→옷 위로 선명한 보지 윤곽",
		"금지: 옷이 끼어 도드라진 보지 윤곽",
		"彼女のお姉ちゃんの→여자친구 언니의",
		"Title 無自覚透け乳首→무방비 유두",
		"여자친구 언니의 무방비 유두와 옷 위로 선명한 보지 윤곽! 더블 유혹에 참지 못한 폭주 피스톤!",
		"開花宣言→벚꽃 개화 발표",
		"紙パン→종이 팬티",
		"万引き→절도|좀도둑질",
		"女子○生→여고생",
		"我慢汁→쿠퍼액",
		"性感開発→성감 개발",
		"敏感なのに更に性感開発→민감한데 성감 개발까지 더해져",
	} {
		assert.Contains(t, rules, expected)
	}
	assert.Less(t, len(rules), 18000)
}

func TestBuildLLMTranslationPrompts_AlwaysIncludesCompressedKoreanRules(t *testing.T) {
	texts := []string{
		"彼女のお姉ちゃんの無自覚透け乳首と食い込みマンスジのW誘惑",
		"開花宣言が出た途端、上司の一声でお花見をすることになった。森に惹かれていく七緒。カラダの関係だけがエスカレートしていく。",
		"我慢汁も精液もドッバドバ 手加減無し20発ぶっこ抜き",
	}
	systemPrompt, _, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		texts,
		[]string{"<<<title[0]>>>", "<<<description[0]>>>", "<<<title[1]>>>"},
	)
	require.NoError(t, err)

	for _, expected := range []string{
		"マンスジ→보지 윤곽",
		"開花宣言→벚꽃 개화 발표",
		"上司の一声→상사의 한마디",
		"惹かれていく→점점 마음이 끌리다",
		"エスカレートしていく→점점 깊어지다",
		"我慢汁→쿠퍼액",
		"ドッバドバ→콸콸",
		"手加減無し→봐주지 않는",
		"鉄マン→강철 보지",
		"桃尻/桃Siri→애플힙",
	} {
		assert.Contains(t, systemPrompt, expected)
	}
	assert.Less(t, len(systemPrompt), 20000)
}

func TestBuildLLMQualityReviewPromptIncludesSourceCandidateAndStrictOutput(t *testing.T) {
	items := []qualityReviewItem{{Source: "鉄マン", Candidate: "철맨"}}
	systemPrompt, userPrompt, err := buildLLMQualityReviewPromptsWithMarkers("ko", items, []string{"<<<quality_review_title>>>"})
	require.NoError(t, err)
	assert.Contains(t, systemPrompt, "mandatory second-pass quality reviewer")
	assert.Contains(t, systemPrompt, "鉄マン")
	assert.Contains(t, systemPrompt, "Every original <<<quality_review_...>>> marker is mandatory")
	assert.Contains(t, systemPrompt, "Never echo [JAPANESE SOURCE] or [KOREAN CANDIDATE]")
	assert.Contains(t, userPrompt, "[JAPANESE SOURCE]\n鉄マン")
	assert.Contains(t, userPrompt, "[KOREAN CANDIDATE]\n철맨")
	assert.Contains(t, userPrompt, "<<<quality_review_title>>>")
	assert.NotContains(t, userPrompt, "[corrected Korean]")
	assert.Equal(t, 1, strings.Count(userPrompt, "<<<quality_review_title>>>"))
	assert.Contains(t, systemPrompt, "桃尻/桃Siri→애플힙")
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
		return nil, &translationError{Kind: TranslationErrorParse, Message: "first output marker not found"}
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

func TestTranslateMovie_RejectsResidualJapaneseAfterRetry(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
		calls++
		return &translationResult{Texts: []string{"격차가 최고すぎる"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true},
	}, provider)
	movie := &models.Movie{Title: "格差が最高すぎる"}

	output, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, warning, "invalid model output")
	assert.Equal(t, 2, calls)
	assert.Equal(t, "格差が最高すぎる", movie.Title, "invalid partial output must not replace the source")
}

func TestTranslateMovie_RejectsPromptTemplateAndKeepsFieldsAtomic(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls++
		if len(texts) == 2 {
			return &translationResult{Texts: []string{"번역된 제목", "[translation]"}}, nil
		}
		return &translationResult{Texts: []string{"[translation]"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true, Description: true},
	}, provider)
	movie := &models.Movie{Title: "原題", Description: "原文の説明"}

	output, _, err := service.TranslateMovie(context.Background(), movie, "")
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "prompt template leaked")
	assert.Equal(t, 2, calls)
	assert.Equal(t, "原題", movie.Title)
	assert.Equal(t, "原文の説明", movie.Description)
}

func TestTranslateMovie_RejectsUnchangedEnglishDescription(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls++
		return &translationResult{Texts: append([]string(nil), texts...)}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "auto", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Description: true},
	}, provider)
	movie := &models.Movie{Description: "An amateur performer visits the studio for her first scene."}

	output, _, err := service.TranslateMovie(context.Background(), movie, "")
	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "unchanged from source")
	assert.Equal(t, 2, calls)
	assert.Equal(t, "An amateur performer visits the studio for her first scene.", movie.Description)
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
