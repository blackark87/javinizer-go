package translation

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestKoreanJAVPromptUsesConciseIntercruralTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	assert.Contains(t, rules, "股コキ→가랑이딸")
	assert.Contains(t, rules, "太ももコキ→허벅지딸")
	assert.Contains(t, rules, "尻コキ→엉덩이딸")
	assert.Contains(t, rules, "금지: 股コキ/마타코키/허벅지 코키/가랑이 성교/허벅지 성교/엉덩이 성교")
	assert.NotContains(t, rules, "가랑이에 끼워 비비기")
}

func TestKoreanJAVPromptCoversMIUM897AndIPX161Context(t *testing.T) {
	rules := koreanJAVPromptRules("ko")

	for _, expected := range []string{
		"意外と推しに弱い is a common 押しに弱い variant/typo",
		"의외로 밀어붙이면 약한",
		"금지: 최애에게 약한",
		"ホテイン→호텔 입성|호텔로 직행",
		"ノリ悪めドライ系女子→반응이 시큰둥한 무심녀|흥 없는 무심녀",
		"エレクト/チンポがエレクトする→발기하다|자지가 서다",
		"금지: 자지를 흥분시키다",
	} {
		assert.Contains(t, rules, expected)
	}
	assert.NotContains(t, rules, "가랑이에 끼워 비비기")
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
	assert.Contains(t, rules, "手コキ/ハンドジョブ/handjob→대딸")
	assert.Contains(t, rules, "금지: 핸드잡")
	assert.NotContains(t, rules, "→핸드잡")
	assert.Contains(t, rules, "シゴく/シゴき→손으로 흔들다|대딸|뽑아주다")
	assert.Contains(t, rules, "舐めシゴき→혀와 손으로 뽑아주기|핥기와 대딸")
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
	assert.Contains(t, rules, "寝取り=남의 파트너 빼앗기")
	assert.Contains(t, rules, "寝取られ願望→아내 뺏기길 바람")
	assert.Contains(t, rules, "もとから寝取られ願望のある男→원래부터 아내 뺏기길 바라는 남자")
	assert.Contains(t, rules, "寝取り屋→아내를 빼앗아주는 업자")
	assert.Contains(t, rules, "旦那→남편")
	assert.Contains(t, rules, "ちんぐり返し=남자를 눕혀")
	assert.Contains(t, rules, "ちんぐり返し騎乗位→남자의 다리를 뒤로 젖힌 기승위")
	assert.Contains(t, rules, "자세명·지배·폭력 창작 금지")
	assert.Contains(t, rules, "금지:친구리/친구리카에시/치무가에리/새우/쟁기/활 비유")
	assert.Contains(t, rules, "タイマン4本番→1대1 본방4회")
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
	assert.Contains(t, userPrompt, "Correct a wrong 역강간 in the candidate")

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
		"only explicit 逆レ/逆レイプ→역강간",
		"Bare レイプ/レ×プ/レ〇プ/レ○プ/レ●プ always→강간, never 역강간",
		"即尺即ハメ→바로 빨고 바로 박기",
		"ベロチュウ→진한 혀키스|딥키스",
		"おっパブ→옵파이 펍|슴가 펍",
		"금지: 오파부",
		"スパンキング→스팽킹",
		"夕美しおん→유미 시온",
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

func TestBuildLLMTranslationPromptsAddsPetitePerformerConstraint(t *testing.T) {
	_, userPrompt, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		[]string{"小柄マンコ貫かれ、小柄ボディに濃厚ぶっかけ！"},
		[]string{"<<<title>>>"},
	)

	require.NoError(t, err)
	assert.Contains(t, userPrompt, "HIGHEST PRIORITY exact phrase: 小柄マンコ貫かれ→아담한 그녀의 보지가 꿰뚫리고")
	assert.Contains(t, userPrompt, "금지: 작은 보지/아담한 보지가")
}

func TestBuildLLMQualityReviewPromptsAddsSourceLocalDirectionConstraint(t *testing.T) {
	_, userPrompt, err := buildLLMQualityReviewPromptsWithMarkers("ko", []qualityReviewItem{{
		Source:    "彼氏裏切りトラウマレ×プ",
		Candidate: "남자친구 배신 트라우마 역강간",
	}}, []string{"<<<quality_review_title>>>"})

	require.NoError(t, err)
	assert.Contains(t, userPrompt, "without an immediately preceding 逆 must be 강간, never 역강간")
	assert.Contains(t, userPrompt, "Correct a wrong 역강간 in the candidate")
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
		"半中半外半彼女→반은 질내·반은 질외·반쪽 여친",
		"円光→조건만남",
		"タダまん→공짜 섹스",
		"ヌける→꼴리는|딸감",
		"種付け→수정섹스|임신시키기",
		"淫裸MIDARA/淫裸（ミダラ）→음란한 알몸",
		"性獣→색마≠성녀",
		"ナンパ→헌팅",
		"パリピ→파티광",
		"セフレちゃん→섹파짱",
		"ヤラせてくれる女→대주는 여자",
	} {
		assert.Contains(t, rules, expected)
	}
	assert.Equal(t, 1, strings.Count(rules, "杭打ち騎乗位→말뚝박기 기승위"))
	assert.Less(t, utf8.RuneCountInString(rules), 10000)
}

func TestKoreanJAVPromptCoversERK091FC2AndSIRO5432(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	for _, expected := range []string{
		"あゆちゃん→아유짱",
		"≠아미유짱",
		"性癖→성벽",
		"居酒屋に誘う→이자카야에 가자고 하다",
		"グビグビ→벌컥벌컥",
		"責めても、責められても→애무해도, 애무받아도",
		"EXACT 小柄マンコ貫かれ→아담한 그녀의 보지가 꿰뚫리고",
		"濃厚ぶっかけ→진한 정액 세례",
		"レビュー特典→리뷰 작성 특전",
		"オホ声→거친 신음",
		"体重37キロの逸材→체중 37kg의 대어",
		"EXACT 見た目とは真逆の超清楚な経験人数3人の彼女とお泊まりSeX→겉모습과 정반대로 남자 경험이 3명뿐인 초청순녀와 숙박 섹스",
		"爆美女→초미녀≠폭녀",
		"ツルツルパイパンの綺麗なマ●コを豪快に広げられ→매끈하고 예쁜 백보지가 과감하게 쫙 벌려지고",
		"潮を部屋中に大噴出→방 안 가득 분수 폭발",
		"潮吹き処女も頂いちゃった模様→생애 첫 분수까지 터뜨려버린 듯",
		"ガン突き激イキ大放出セックス→쑤셔박기·격렬 절정·분수 대방출 섹스",
		"Truncated →A stays →A",
	} {
		assert.Contains(t, rules, expected)
	}
	assert.NotContains(t, rules, "オホ声→오호 신음")
	assert.NotContains(t, rules, "性癖→성적 취향")
	assert.NotContains(t, rules, "→첫 분수 경험까지 빼앗은 모양")
	assert.NotContains(t, rules, "→첫 분수까지 따먹은 듯")
	assert.NotContains(t, rules, "거칠게 박아 격렬하게 가버리고 마구 쏟아내는 섹스")
	assert.Less(t, utf8.RuneCountInString(rules), 10000)
}

func TestKoreanJAVPromptCoversLatestProductionMistranslations(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	for _, expected := range []string{
		"げんえ./き→げんえき→현역",
		"금지: 음란녀/경험 있음",
		"秘蔵→미공개|비공개 소장",
		"금지: 비장",
		"蔵出し/蔵出し動画→미공개 영상|소장 영상 공개",
		"ガルバ→걸즈바",
		"気弱な→소심한",
		"sports シュート→슛",
		"JAV double meaning シュートを決める→한 발 쏘다",
		"Aにシュートを決める→A에게 한 발 쏘다",
		"EXACT チームを勝利に導くマネージャーに華麗なシュートを決めてきました→팀을 승리로 이끄는 매니저에게 제대로 한 발 쏘고 왔습니다",
		"금지: 슈트/화려한 슛/매니저가 한 발 쏘다",
		"小動物系→소동물계",
		"小動物系美少女→소동물계 미소녀",
		"금지: 작고 귀여운으로 설명",
		"利き手→주로 쓰는 손",
		"좌우를 임의로 만들지 않는다",
		"erotic-change 確変",
	} {
		assert.Contains(t, rules, expected)
	}
}

func TestBuildLLMPrompts_DictionaryModeUsesCompactPromptAndSkipsLegacyConstraints(t *testing.T) {
	options := llmPromptOptions{
		dictionaryEnabled: true,
		dictionary:        "ギャルしべ長者 -> 갸루 소개 릴레이\n中出し -> 질내사정",
	}
	systemPrompt, userPrompt, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		[]string{"ギャルしべ長者で中出し"},
		[]string{"<<<title>>>"},
		options,
	)
	require.NoError(t, err)

	assert.Contains(t, systemPrompt, "Korean JAV dictionary mode:")
	assert.Contains(t, systemPrompt, "USER JAV DICTIONARY")
	assert.Contains(t, systemPrompt, "Translate each labeled section independently")
	assert.Contains(t, systemPrompt, "preserve IDs and numbers")
	assert.Contains(t, systemPrompt, "never complete a source that ends truncated")
	assert.Contains(t, systemPrompt, "ギャルしべ長者 -> 갸루 소개 릴레이")
	assert.Contains(t, systemPrompt, "中出し -> 질내사정")
	assert.NotContains(t, systemPrompt, "鉄マン→강철 보지")
	assert.NotContains(t, userPrompt, "BATCH TERM CHECK")
	assert.Contains(t, userPrompt, "ギャルしべ長者で中出し")

	reviewPrompt, reviewUserPrompt, err := buildLLMQualityReviewPromptsWithMarkers(
		"ko",
		[]qualityReviewItem{{Source: "ギャルしべ長者", Candidate: "갸루시베 장자"}},
		[]string{"<<<quality_review_title>>>"},
		options,
	)
	require.NoError(t, err)
	assert.Contains(t, reviewPrompt, "Korean JAV dictionary mode:")
	assert.Contains(t, reviewPrompt, "ギャルしべ長者 -> 갸루 소개 릴레이")
	assert.NotContains(t, reviewPrompt, "鉄マン→강철 보지")
	assert.NotContains(t, reviewUserPrompt, "BATCH TERM CHECK")
}

func TestBuildLLMPrompts_DisabledDictionaryKeepsFullPrompt(t *testing.T) {
	systemPrompt, userPrompt, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		[]string{"ギャルしべ長者"},
		[]string{"<<<title>>>"},
		llmPromptOptions{dictionary: "中出し -> 안에 싸기"},
	)
	require.NoError(t, err)

	assert.Contains(t, systemPrompt, "鉄マン→강철 보지")
	assert.NotContains(t, systemPrompt, "USER JAV DICTIONARY")
	assert.Contains(t, userPrompt, "BATCH TERM CHECK")
}

func TestBuildLLMPrompts_RemoveScrapedSiteAttributionFromTitles(t *testing.T) {
	systemPrompt, _, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		[]string{"作品名：Mgs動画＜プレステージ グループ＞アダルト動画配信サイト"},
		[]string{"<<<title>>>"},
		llmPromptOptions{dictionaryEnabled: true},
	)
	require.NoError(t, err)
	assert.Contains(t, systemPrompt, "trailing source-site, streaming-platform, or store attribution")
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
	assert.Less(t, utf8.RuneCountInString(systemPrompt), 12000)
}

func TestBuildLLMQualityReviewPromptIncludesSourceCandidateAndStrictOutput(t *testing.T) {
	items := []qualityReviewItem{{Source: "鉄マン", Candidate: "철맨"}}
	systemPrompt, userPrompt, err := buildLLMQualityReviewPromptsWithMarkers("ko", items, []string{"<<<quality_review_title>>>"})
	require.NoError(t, err)
	assert.Contains(t, systemPrompt, "mandatory second-pass quality reviewer")
	assert.Contains(t, systemPrompt, "鉄マン")
	assert.Contains(t, systemPrompt, "Copy every <<<quality_review_...>>> marker")
	assert.Contains(t, systemPrompt, llmCompletionMarker)
	assert.Contains(t, systemPrompt, "Never echo source/candidate labels")
	assert.Contains(t, systemPrompt, "Do not restore omitted release tags")
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

func TestSanitizeQualityReviewTextExtractsRewrittenDescription(t *testing.T) {
	first := "카린짱이 파르기 떨며 패배 절정해버린다."
	rewritten := "카린짱이 파르르 떨며 패배 절정해버린다."
	value := first + "\n\n[REWRITTEN_KOREAN_DESCRIPTION]\n" + rewritten

	assert.Equal(t, rewritten, sanitizeQualityReviewTextWithCandidate(value, first))
	assert.True(t, isInvalidQualityReviewText(value), "unextracted rewrite labels must be rejected")
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

func TestSanitizeQualityReviewTextExtractsUnlabelledFinalKoreanLine(t *testing.T) {
	echoed := "おっパブで即尺即ハメ\n옵파이 펍에서 바로 빨고 바로 박기"
	assert.Equal(t, "옵파이 펍에서 바로 빨고 바로 박기",
		sanitizeQualityReviewTextWithCandidate(echoed, "기존 후보"))
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

func TestReviewJAVTranslationsFallsBackFromInventedLatinFragment(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "자신의 성벽을 말해주는 솔ert하고 야한 여자아이"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)
	candidate := "자신의 성벽을 말해주는 솔직하고 야한 여자아이"

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_description",
		Source:    "自分の性癖を話してくれる正直でエロい女の子",
		Candidate: candidate,
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{candidate}, result)
}

func TestReviewJAVTranslationsCleansPromotionalSourceBeforeSecondPass(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "【전속 제2탄!】 본편 설명"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_description",
		Source: "特典・セット商品イメージ 特典・セット商品情報 【特典内容】 ・生写真2枚 " +
			"特典付き商品・セット商品について【専属第2弾！】本編の説明。" +
			"※こちらはBlu-ray Disc専用ソフトです。対応プレイヤー以外では再生できません。",
		Candidate: "【전속 제2탄!】 본편 설명",
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"【전속 제2탄!】 본편 설명"}, result)
	require.Len(t, provider.items, 1)
	assert.Equal(t, "【専属第2弾！】本編の説明。", provider.items[0].Source)
}

func TestReviewJAVTranslationsAcceptsFinalTextAfterGemmaPromptEcho(t *testing.T) {
	candidate := "청춘 교복 미소녀와 보내는 성춘 ⟦8001⟧SEX. ⟦8002⟧분 ⟦7000⟧"
	provider := &qualityReviewMockProvider{response: "[JAPANESE SOURCE]\nアオハル 制服美少女 ⟦8001⟧SEX ⟦8002⟧分 ⟦7000⟧\n" +
		"[KOREAN CANDIDATE]\n" + candidate + "\n\n" +
		candidate}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "アオハル 制服美少女 3SEX 160分 流川夕",
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

func TestReviewJAVTranslationsProtectsImmutableMetadataTokens(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "교정된 제목 ⟦8000⟧ ⟦8001⟧"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "NTK-729 case08 原題",
		Candidate: "NTK-729 case08 기존 제목",
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"교정된 제목 NTK-729 case08"}, result)
	require.Len(t, provider.items, 1)
	assert.Equal(t, "⟦8000⟧ ⟦8001⟧ 原題", provider.items[0].Source)
	assert.Equal(t, "⟦8000⟧ ⟦8001⟧ 기존 제목", provider.items[0].Candidate)
}

func TestReviewJAVTranslationsProtectsTitleNumbers(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "갸루시베 장자 ⟦8000⟧ 엄선한 갸루 ⟦8001⟧명, ⟦8002⟧분"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	result, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "ギャルしべ長者 13 極選エロギャル3名245分",
		Candidate: "갸루시베 장자 13 엄선한 갸루 3명, 245분",
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"갸루시베 장자 13 엄선한 갸루 3명, 245분"}, result)
	require.Len(t, provider.items, 1)
	assert.Equal(t, "ギャルしべ長者 ⟦8000⟧ 極選エロギャル⟦8001⟧名⟦8002⟧分", provider.items[0].Source)
	assert.Equal(t, "갸루시베 장자 ⟦8000⟧ 엄선한 갸루 ⟦8001⟧명, ⟦8002⟧분", provider.items[0].Candidate)
}

func TestReviewJAVTranslationsRejectsCandidateWithChangedTitleNumbers(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "사용되지 않아야 하는 응답"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	_, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "極選エロギャル3名245分",
		Candidate: "엄선한 야한 갸루 3명 2량 245분",
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "candidate changed numeric metadata")
	assert.Empty(t, provider.items)
}

func TestReviewJAVTranslationsRejectsReviewerAddedTitleNumber(t *testing.T) {
	provider := &qualityReviewMockProvider{response: "엄선한 갸루 ⟦8000⟧명, ⟦8001⟧분 2량"}
	service := New(Config{Enabled: true, Provider: "openai-compatible", TargetLanguage: "ko"}, provider)

	_, err := service.ReviewJAVTranslations(context.Background(), []QualityReviewField{{
		FieldName: "quality_review_title",
		Source:    "極選エロギャル3名245分",
		Candidate: "엄선한 야한 갸루 3명, 245분",
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reviewer changed numeric metadata")
}

func TestBuildTranslationPlanProtectsTitleNumbersAndDescriptionMetadataNumbers(t *testing.T) {
	service := New(Config{Enabled: true, Fields: fieldsConfig{Title: true, Description: true}})
	plan := service.BuildTranslationPlan(&models.Movie{
		Title:       "ギャルしべ長者 13 極選エロギャル3名245分",
		Description: "1人目を紹介。収録時間は245分。",
	}, "ko", "ja", "test")
	require.Len(t, plan.Fields, 2)

	title := plan.Fields[0]
	assert.NotContains(t, title.Text, "13")
	assert.NotContains(t, title.Text, "245")
	assert.ElementsMatch(t, []string{"13", "3", "245"}, mapValues(title.ImmutablePlaceholders))

	description := plan.Fields[1]
	assert.Contains(t, description.Text, "1人目")
	assert.NotContains(t, description.Text, "245分")
	assert.ElementsMatch(t, []string{"245"}, mapValues(description.ImmutablePlaceholders))
}

func TestNormalizeKoreanTitleSeparatorsFormatsCountAndRuntime(t *testing.T) {
	assert.Equal(
		t,
		"엄선한 야한 갸루 3명, 245분",
		normalizeKoreanTitleSeparators("엄선한 야한 갸루 3명 245분"),
	)
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func TestTranslateMovieRetriesAndRestoresImmutableMetadataTokens(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
		calls++
		if calls == 1 {
			return &translationResult{Texts: []string{"번역 제목 ⟦9000⟧"}}, nil
		}
		return &translationResult{Texts: []string{"번역 제목 ⟦9000⟧ ⟦9000⟧"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true},
	}, provider)
	movie := &models.Movie{Title: "NTK-729 NTK-729 原題"}

	output, warning, err := service.TranslateMovie(context.Background(), movie, "")

	require.NoError(t, err)
	assert.Empty(t, warning)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "번역 제목 NTK-729 NTK-729", movie.Title)
	assert.Equal(t, "번역 제목 NTK-729 NTK-729", output.Movie.Title)
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
