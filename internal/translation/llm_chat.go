package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// openAIChatRequest represents a chat completion request for OpenAI-compatible APIs.
type openAIChatRequest struct {
	Model              string              `json:"model"`
	Temperature        float64             `json:"temperature"`
	MaxTokens          int                 `json:"max_tokens,omitempty"`
	Messages           []openAIChatMessage `json:"messages"`
	ChatTemplateKwargs map[string]any      `json:"chat_template_kwargs,omitempty"`
	ReasoningEffort    string              `json:"reasoning_effort,omitempty"`
	EnableThinking     *bool               `json:"enable_thinking,omitempty"`
}

// openAIChatMessage represents a single message in a chat request.
type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatResponse represents a chat completion response from OpenAI-compatible APIs.
type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// openAIChatCallOptions configures an OpenAI-compatible chat translation call.
type openAIChatCallOptions struct {
	provider  string
	baseURL   string
	endpoint  string
	model     string
	headers   map[string]string
	request   openAIChatRequest
	textCount int
	markers   []string
	logInput  bool
	logTiming bool
}

type translationMarkersContextKey struct{}

type qualityReviewContextKey struct{}

type qualityReviewItem struct {
	Source    string
	Candidate string
}

const llmCompletionMarker = "<<<JZ_DONE>>>"

func withQualityReview(ctx context.Context, items []qualityReviewItem) context.Context {
	return context.WithValue(ctx, qualityReviewContextKey{}, append([]qualityReviewItem(nil), items...))
}

func qualityReviewFromContext(ctx context.Context, count int) ([]qualityReviewItem, bool) {
	if ctx == nil {
		return nil, false
	}
	items, ok := ctx.Value(qualityReviewContextKey{}).([]qualityReviewItem)
	return items, ok && len(items) == count
}

func withTranslationMarkers(ctx context.Context, fieldNames []string) context.Context {
	markers := make([]string, len(fieldNames))
	for i, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			fieldName = fmt.Sprintf("JZ_%d", i)
		}
		markers[i] = "<<<" + fieldName + ">>>"
	}
	return context.WithValue(ctx, translationMarkersContextKey{}, markers)
}

func translationMarkersFromContext(ctx context.Context, count int) []string {
	if ctx != nil {
		if markers, ok := ctx.Value(translationMarkersContextKey{}).([]string); ok && len(markers) == count {
			return append([]string(nil), markers...)
		}
	}
	return indexedTranslationMarkers(count)
}

// LLMChatAdapter abstracts the provider-specific request/response format for LLM
// chat-based translation. OpenAI and Anthropic implement this interface so that
// the shared executeLLMChatTranslation pipeline can be reused across providers.
type LLMChatAdapter interface {
	// BuildRequest constructs the provider-specific HTTP request for chat translation.
	BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error)
	// DecodeResponse parses the provider-specific HTTP response body into a translationResult.
	DecodeResponse(providerName string, respBody []byte, textCount int) (*translationResult, error)
}

func buildLLMTranslationPromptsWithMarkers(sourceLang, targetLang string, texts, markers []string) (string, string, error) {
	if len(texts) == 0 || len(markers) != len(texts) {
		return "", "", fmt.Errorf("translation prompt requires one marker per text (%d markers for %d texts)", len(markers), len(texts))
	}

	terminologyRules := "Translate every meaningful source segment. Use established current target-language JAV terminology and render ordinary words, idioms, sound words, and transparent compounds by meaning. Transliterate only person or brand names, opaque proper nouns, and genuine industry loanwords; leave no Japanese script in non-Japanese output except protected name punctuation. "
	personNameRule := "Person-name rule: <<<actress[N]>>> and <<<title_as_name>>> contain one performer. Transliterate the reading in Japanese FamilyName GivenName order; romaji is authoritative. Never invent, anglicize, or substitute a different performer name; never shorten or translate it or turn kanji into emoji. Preserve the middle dot ・ inside one name, never turn it into a comma, and never split one performer. Apply this rule to a short name-like <<<title>>> too. "
	properNounRule := "Proper-noun rule: <<<maker>>>, <<<label>>>, and <<<director>>> are names. Transliterate them phonetically and do not embellish them. "
	cleanupRules := "Title cleanup: remove bracketed VR/release labels such as [VR], 【VR】, and 【8K VR】. Description cleanup: remove playback/device notices, VR-only notices, platform notices, sales campaigns, and store promotions; if only excluded material remains, return an empty section. "
	placeholderRule := "Any Hangul already present is final and must be copied verbatim. Protected tokens of the form ⟦N⟧ must be reproduced exactly and never translated, removed, or renumbered. "
	koreanRules := koreanJAVPromptRules(targetLang)

	systemPrompt := fmt.Sprintf("You translate Japanese adult video (JAV) metadata and AV-studio metadata for actual studio use. Write concise contemporary titles and complete natural descriptions; avoid corny, dated, literary, moralizing, euphemistic, or invented wording. %s%s%s%s%s%sReturn marker+one-line translation for each, then exact final line %s; no JSON or commentary. Source: %s. Target: %s.", terminologyRules, koreanRules, personNameRule, properNounRule, cleanupRules, placeholderRule, llmCompletionMarker, sourceLang, targetLang)

	var userPrompt strings.Builder
	userPrompt.WriteString("Translate each labeled section below:\n")
	for i, text := range texts {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteByte('\n')
		userPrompt.WriteString(text)
		userPrompt.WriteByte('\n')
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func buildLLMQualityReviewPromptsWithMarkers(targetLang string, items []qualityReviewItem, markers []string) (string, string, error) {
	if len(items) == 0 || len(markers) != len(items) {
		return "", "", fmt.Errorf("quality review prompt requires one marker per item (%d markers for %d items)", len(markers), len(items))
	}
	systemPrompt := "You are the mandatory second-pass quality reviewer for Japanese AV metadata translated into Korean. Compare source and candidate, then silently fix mistranslation, calques, untranslated or transliterated slang, omissions, inventions, broken text, awkward grammar, and outdated terminology. Preserve explicitness, tone, protected tokens, and performer identity. Do not restore omitted release tags, playback/device notices, or sales/store promotions. Return the complete corrected Korean text, not an assessment. " + koreanJAVPromptRules(targetLang) + "Copy every <<<quality_review_...>>> marker with its complete corrected Korean text, then exact final line " + llmCompletionMarker + ". Never echo source/candidate labels or add commentary."

	var userPrompt strings.Builder
	userPrompt.WriteString("Review and, where necessary, rewrite each candidate by comparing it with its Japanese source:\n")
	for i, item := range items {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteString("\n[JAPANESE SOURCE]\n")
		userPrompt.WriteString(item.Source)
		userPrompt.WriteString("\n[KOREAN CANDIDATE]\n")
		userPrompt.WriteString(item.Candidate)
		userPrompt.WriteByte('\n')
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func koreanJAVPromptRules(targetLang string) string {
	lang := strings.ToLower(strings.TrimSpace(targetLang))
	if lang != "ko" && !strings.HasPrefix(lang, "ko-") && !strings.HasPrefix(lang, "ko_") {
		return ""
	}

	rules := []string{
		"Korean JAV: preserve meaning/direction/tone; no add/omit. Use concise Korean AV. Translate language/idioms/compounds/sounds by meaning; transliterate only names/loanwords. Avoid calques/euphemisms/dated/invented terms/Japanese leftovers. A→B|C context.",
		"HIGHEST PRIORITY exact form: ガチ恋営業chu→진심인 척하는 영업 츄. Latin suffix chu is a kiss sound, not Japanese 中 or Korean 중; the final syllable must be 츄. 금지: 영업 중/가치코이/가치코이 영업 중/chu omission.",
		"数珠つなぎ→릴레이|연속; たすきリレー/バトンリレー→바통 터치|릴레이; 芋づる式→연쇄|연속; ハシゴ酒→술집 투어|술집 순례; 朝までハシゴ酒→밤새 술집 투어, 금지: 아침까지 하시고주.",
		"パパ活/Sugar Dating→스폰|조건; 一本釣り→독점 스카우트|길거리 캐스팅; 箱入り/箱入り娘→아가씨|순진녀; 逆指名→여배우의 선택|역지명; 垢抜け→비주얼 업그레이드|세련된; 初々しい→풋풋한|앳된; 玄人/玄人肌→프로|능숙한.",
		"中出し/Creampie→질내사정; 顔射/Facial→안면사정; ぶっかけ/Bukkake→정액 세례|붓카케; 個撮→개인촬영; ハメ撮り→POV|셀프카메라; 汁男優→사정 전문 남배우.",
		"顔騎→안면기승, 금지: 페이스시팅/얼굴 기승위; 足裏→발바닥; 足コキ→풋잡, 금지: 발코키; ムレた足裏→땀에 찬 발바닥|땀이 밴 발바닥, 금지: 눅눅한 발바닥; 足裏からつま先を味わい尽くす→발바닥부터 발끝까지 샅샅이 맛본다.",
		"桃尻/桃Siri→애플힙; never treat as a person name or write 모모시리.",
		"ミルチオ→미루치오 (coinage miru+フェラチオ), 금지: 밀치오; ミルチオの愛人→미루치오를 해주는 불륜 상대, 금지: 미루치오의 정부; adultery 愛人→불륜 상대, 금지: 정부/애인; female 絶倫性欲者→절륜 색녀, male→절륜남, 금지: 절륜 성욕자.",
		"シゴく/シゴき→손으로 흔들다|핸드잡|뽑아주다; 舐めシゴき→혀와 손으로 뽑아주기|핥기와 핸드잡; 舐めシゴきフルコース→혀와 손으로 뽑아주는 풀코스. 금지: 시고키/핥기 시고키. 愛ベロ/愛舐め의 愛는 테크닉 강조이며 사랑의로 기계 번역하지 않는다.",
		"エビ反り→허리가 휘는; エビ反り痙攣絶頂→허리가 휘며 경련 절정; エビ反りオーガズム→허리가 휘는 오르가슴. 금지: 허리 꺾인/새우등처럼 휜/등을 활처럼 휘는/허리를 뒤로 젖히는/에비반리/에비소리. erotic massage 特別施術→특별 코스, 금지: 특별 시술/특별 트리트먼트.",
		"人妻→유부녀; 人妻もの→유부녀물; 금지: 인처. セフレ→섹스 파트너|섹파, 금지: 세프레; セフレ志願の女の子→섹파를 자처하는 여자|섹스 파트너를 원하는 여자; ちんちんおっきしたら→자지가 서면, 금지: n자지 커지면.",
		"寸止め→절정 직전 멈추기|절정 직전까지 애태우기|절정을 참게 하는, 금지: 에징; リモバイ→리모트 바이브, 금지: 리모바이; 寸止めリモバイ調教→리모트 바이브로 절정 직전까지 애태우는 조교; 小鹿アクメ→다리가 후들거리는 절정|무릎이 풀리는 절정; 膝ガクガク小鹿アクメ→무릎이 후들거리는 절정, 금지: 새끼 사슴 오르가슴.",
		"秘部→은밀한 곳|민감한 부위, 금지: 비부; きわどい秘部を触られすぎて→은밀한 곳을 집요하게 만져져. prose 寝取られました→다른 남자에게 넘어가 버렸다처럼 사건을 자연스럽게 표현하고 네토라레 당했습니다로 음차하지 않는다; NTR은 장르 라벨일 때만 유지.",
		"sexual-trait prefix ド intensifies: ド痴女→극강의 치녀|지독한 색녀, 금지: 도치녀; ド変態→극도의 변태|지독한 변태; 結婚した妻→아내, 금지: 결혼한 아내; 性欲おさまらない→멈출 줄 모르는 성욕|주체할 수 없는 성욕.",
		"言いなり/イイナリ→시키는 대로 하는|말이라면 뭐든 따르는|복종하는, 금지: 이이나리; ドM→극M|극도의 마조, 금지: 도M; イイナリドM→말이라면 뭐든 따르는 극M|복종하는 극M.",
		"逆パコ→여자가 덮치는|여배우가 덮치는|여자 주도 섹스, 금지: 역파코; 痴女られる→치녀에게 농락당하다; がっつり痴女られたい→치녀에게 실컷 농락당하고 싶다, 금지: 듬뿍 치녀 취급당하고 싶다.",
		"逆レ/逆レイプ→역강간|여자가 강제로 덮치는; 逆レ搾精→역강간 착정|여자가 강제로 덮쳐 정액을 뽑아내기. 逆パコ와 구분하며 source에 パコ가 없으면 역파코/파코를 넣지 않는다.",
		"パコ/パコる/パコパコ→섹스/섹스하다/박아대다, 금지: 파코; イキパコ→절정 섹스|가버리는 섹스; オフパコ→비밀 만남 섹스|팬과의 섹스; 生パコ→노콘 섹스; イチャパコ→달달한 섹스; パコパコ撮影→마구 섹스하는 촬영.",
		"ポルチオ→깊숙한 피스톤|질 깊숙이 파고드는 피스톤|질 깊은 곳을 자극하다. 금지: 강타하다/집중 공략하다/자궁경부/질 안쪽을 찌르다.",
		"ジュボジュボ: penis sucking→자지를 질척하게 빨아대다; penis licking→자지를 침 범벅으로 핥아대다; body licking→축축하게 핥아대다; 금지: 쥬보쥬보. aggressive 1発ハメる→한 번 따먹다, 금지: 한 판 박아버리다.",
		"Japanese sexual sounds must describe action/result, never Hangul sound transliteration: ドピュドピュ→연속 사정|정액을 연달아 뿜다; じゅぽじゅぽ/じゅっぽんじゅっぽん/グポグポ/ジュルル→질척하게 빨아대다|입 깊숙이 삼켜 빨아대다; ズボズボ→깊숙이 박히는 피스톤; チュパチュパ/ペロちゅぱ→진하게 빨아대다|핥고 빨아대다. 금지: 도퓨도퓨/쥬폰쥬폰/쥬퓻쥬퓻/쥬포쥬포/쥬르르/즈보즈보/츄파츄파/페로츄파/츄릅츄릅.",
		"numeral+穴 counts sexual orifices: compressed title→홀, prose→구멍, 금지: 혈/untranslated 穴; 3穴→3홀|세 구멍; 2穴セフレ→2홀 섹파|두 구멍을 내주는 섹파. semen-context ごっくん→정액 삼키기|정액을 삼키다, 금지: 고쿤; ノドマンコ→목구멍; ケツマンコ→후장, 금지: 목구멍 보지/똥보지.",
		"ストゼロ is Strong Zero→스트롱 제로, 금지: 스트로제로/스트로 제로; 潮吹き→분수|애액 분출|애액을 뿜다, 금지: 스포팅/음차; 限界ストゼロ潮吹きFUCK→스트롱 제로를 마시며 한계까지 분수를 뿜는 섹스.",
		"イクイク→연속 절정|계속 가버리는; プリプリ尻→탱탱한 엉덩이; デレデレ→푹 빠진|애정 가득한; エロエロ→음란한; sexual チンしゃぶ→펠라|자지를 핥고 빨다, 금지: 자지 샤브샤브.",
		"sexual おしゃぶり→펠라|자지 빨기, not pacifier unless scene explicitly shows one; 吸引おしゃぶり→빨아들이는 펠라|강하게 빨아대는 펠라, 금지: 흡입 오샤부리.",
		"鉄マン→강철 보지, 금지: 철맨; マジかよ！？→실화냐?!|말도 안 돼?!; 秘技教本→비법 교본, 금지: 비기 교본.",
		"生ハメ→노콘, 금지: 생하메/생삽입/생으로 하메; 生ハメSEX→노콘 섹스; 生ハメ中出し→노콘 질내사정; シコサポ/オナサポ/オナニーサポート→자위 서포트, 금지: 시코사포/오나사포.",
		"媚薬→최음제, 금지: 피임약; キメセク: with 媚薬→최음제에 취한 섹스|최음제 섹스, drugs/unspecified→약에 취한 섹스|약물 섹스, 금지: 키메섹/킴세쿠/키메세쿠; キメセクの巣→약물 섹스의 소굴; drug-context ガンギマリ→약에 완전히 취한|약기운이 제대로 오른, 금지: 완전히 맛이 간.",
		"タイパ→시간 효율|시간 대비 효율, 금지: 타이파; タイパを気にし過ぎる→시간 효율을 지나치게 따지는. フェザータッチ/フェザー→깃털처럼 살살 닿는 애무; フェザー指コキ→깃털처럼 살살 애태우는 손가락 자위|손가락으로 살살 흔들어주기, 금지: 페더 손가락 핸드잡/페더 지코키/손가락 핸드잡. ejaculation 暴発→참지 못하고 사정하다|터뜨리다, 금지: 폭발/폭발 교육.",
		"erotic hotel/bed/body 水浸し without real flood→침대가 흠뻑 젖도록|러브호텔을 흠뻑 적시며|애액으로 흠뻑 젖은; 금지: 러브호텔 물바다.",
		"ヤリマン→문란녀|헤픈 여자|아무하고나 자는 여자, 금지: 야리만; 逆ナン/逆ナンパ→여자가 남자를 헌팅하는|남자 사냥, 금지: 역나/역난/역지명; 逆ナンドライブ→남자를 헌팅하는 드라이브|남자 사냥 드라이브. AV-title 淫行→섹스|남자를 꼬셔 섹스하는, use 음행 only for clear legal misconduct. 甘サド→달콤하게 괴롭히는 S|상냥한 S플레이, 금지: 달콤 사디스틱. 杭打ちピストン→위에서 거칠게 내리꽂는 피스톤|찍어 누르는 피스톤, 금지: 말뚝박기 피스톤; 杭打ち騎乗位→말뚝박기 기승위.",
		"しろーと/素人→아마추어|일반인, 금지: 시로토/시로트; キャバ嬢→캬바걸|캬바클럽 호스티스, 금지: 캬바죠/카바죠; 美乳/超美乳→예쁜 가슴|아름다운 가슴|매우 아름다운 가슴, 금지: 미유/초미유; インフルエンサー→인플루언서, 금지: 인플루큐언서.",
		"body スタイル/体型/typo 体系→몸매|체형, not 스타일/체계; スタイル最強/最強スタイル→최강 몸매; 理想のモテ体型/理想のモテ体系→이상적인 인기 몸매; praise 極上→최고|최상급, 금지: 극상; エロい→야한, 금지: 에로한; 極エロ→극도로 야한|극강의 야함, 금지: 극에로; びんびんフル勃起→빳빳하게 완전 발기, 금지: 풀 발기.",
		"寝取り=남의 파트너 빼앗기;寝取られ=자기 파트너 빼앗김;寝取られ願望→아내 뺏기길 바람;もとから寝取られ願望のある男→원래부터 아내 뺏기길 바라는 남자;寝取り屋→아내를 빼앗아주는 업자;旦那→남편;방향 보존,寝取り≠네토라레.ちんぐり返し=남자를 눕혀 다리를 머리 쪽으로 젖혀 엉덩이/애널 노출;자세명·지배·폭력 창작 금지.ちんぐり返し騎乗位→남자의 다리를 뒤로 젖힌 기승위;ちんぐり返しアナル舐め→남자의 다리를 뒤로 젖혀 애널 핥기.금지:친구리/친구리카에시/치무가에리/새우/쟁기/활 비유.タイマン→1대1 맞대결|승부;タイマン4本番→1대1 본방4회.",
		"胸糞→역겨운|기분 더러운; 胸糞NTR→역겨운 NTR|기분 더러운 NTR; 鬱勃起→우울한데도 발기되는|우울 발기, 금지: 울울한 발기; NTR copy 壊される→망가지다|망가뜨리다, never omit.",
		"sexual-prose 蜜壺→보지|질, 금지: 밀통/꿀단지/비부; another-person 手マン→핑거링|손가락으로 보지를 자극하다, 금지: 손가락 자위; 美意識溢れる体→아름답게 가꾼 몸, 금지: 미적 감각이 넘치는 몸; keyword 浅草→아사쿠사; ordinary fortune 大吉→대길|대박, use 다이키치 only for a person.",
		"玩具責め→성인용품 공세|장난감 조교, 금지: 장난감 괴롭히기/장난감 괴롭힘; 確定ビッチ→확실한 문란녀, 금지: 확정 비치; female-climax 大・連・発/大連発→연속 절정, 금지: 연속 사정/untranslated 대·연·발; ヤリモクインフルエンサー→섹스만 노리는 인플루언서, 금지: 섹스 목적인 인플루언서.",
		"POV 完全主観→완전 1인칭 시점, 금지: 완전 주관; 青春グラフィティ→청춘 이야기|청춘 기록, 금지: 청춘 그래피티; とびきりエッチ→아주 야한|유난히 야한, 금지: 아주 특별한; preserve 青春/性春 wordplay as 청춘/성춘; 女優の本音と女優の本気→여배우의 솔직한 속마음과 진짜 모습, 금지: 진심과 진지함.",
		"slang-suffix 沼→푹 빠지는|헤어나올 수 없는, 금지: literal 늪; ビッチ沼→문란녀에게 푹 빠지는|헤어나올 수 없는 문란녀의 매력. casual-sex-partner 都合のイイ→원할 때 만날 수 있는|필요할 때 만나는, 금지: 편리한/조건 좋은. entertainment/idol 枕営業→성상납, not 스폰; 濃交→농밀한 교감|진한 교감, 금지: 농교; 色白→하얀 피부|피부가 하얀, 금지: 색백; 美巨乳→예쁜 거유|아름다운 거유, 금지: 미거유; エロかわ/エロ可愛い→야하고 귀여운, 금지: 에로 귀여운.",
		"erotic-service 乳首エステ→유두 마사지, 금지: 유두 에스테/특별 코스; 舐めテク/ハンドテク→혀 테크닉/손 테크닉, 금지: 핥기 기술/핸드 기술; 僕の身代わりに→나 대신, where の is not possessive 내; バクヌキ→실컷 빼주는|마구 뽑아주는, 금지: 바쿠누키; 挟射→가슴 사이에 끼워 사정; sexual-disparaging よわよわ→허접|형편없는, not physically weak; おま○こよわよわ→보지 허접, 금지: 보지 약한.",
		"彼女のお姉ちゃん→여자친구 언니; 彼女のお姉ちゃんの→여자친구 언니의, 금지: 그녀의 누나/그녀의 언니. Title 無自覚透け乳首→무방비 유두, 금지: 자신도 모르게 비치는 유두. マンスジ→보지 윤곽|도드라진 보지 라인, 금지: 만스지/romanized gloss; title 食い込みマンスジ→옷 위로 선명한 보지 윤곽, 금지: 옷이 끼어 도드라진 보지 윤곽/끼어들어/파고드는 보지 라인. Exact compressed title: 彼女のお姉ちゃんの無自覚透け乳首と食い込みマンスジのW誘惑にガマンできずに暴走ピストン！→여자친구 언니의 무방비 유두와 옷 위로 선명한 보지 윤곽! 더블 유혹에 참지 못한 폭주 피스톤! 開花宣言→벚꽃 개화 발표, 금지: 꽃구경 선언; 上司の一声→상사의 한마디|상사의 제안; 惹かれていく→점점 마음이 끌리다, 금지: 끌려가다; relationship エスカレートしていく→점점 깊어지다|격해지다, 금지: 에스컬레이트.",
		"esthetic/massage 紙パン→종이 팬티; never drop 紙. 万引き→절도|좀도둑질, 금지: 만행/invented 쇼핑몰; 万引きの罪→절도죄; censored 女子○生→여고생, 금지: 여대생; ケツ穴→애널|후장. 我慢汁→쿠퍼액, 금지: 애액/쿠모리액; ドッバドバ→콸콸|마구 쏟아지는; 手加減無し→봐주지 않는|가차 없는, 금지: 자제 없이.",
		"敏感なのに更に性感開発→민감한데 성감 개발까지 더해져; 性感開発→성감 개발; 連続イキ→연속 절정; 大絶頂アクメ→강렬한 절정; count 3本番→본방 3회.",
		"雑魚: fish→잡어, sexual insult→허접|하찮은|찌질한; 雑魚チ●ポ→허접 자지, 금지: 잡어 자지. 食い意地: food→식탐; when governing 肉棒/巨根/sex→욕정; 喰い意地爆発→욕정 폭발, 금지: 식탐 폭발.",
		"むしゃぶりつく: translate fluently by object/action, never use food-like 게걸스럽게. ASMR compounds describe act+sound: ベチョレロ唾液チ〇ポ咀嚼→타액 범벅 펠라 소리; ヌチュグチュ粘着マン汁音→끈적한 애액이 질척이는 소리; 금지: 자지 저작/invented trailing 섹스!.",
		"name honorifics: さん/氏→씨; 様→님; ちゃん/たん→짱; くん/君→군; みあたん→미아짱, 금지: 미아탄/미아상/미아사마. Grammatical 様 meaning 모습 is not an honorific.",
		"半中半外半彼女→반은 질내·반은 질외·반쪽 여친;円光→조건만남;タダまん→공짜 섹스;ヌける→꼴리는|딸감;種付け→수정섹스|임신시키기;淫裸MIDARA/淫裸（ミダラ）→음란한 알몸;性獣→색마≠성녀;ナンパ→헌팅;パリピ→파티광;セフレちゃん→섹파짱;ヤラせてくれる女→대주는 여자.",
		"Explicit/censored anatomy: ま〇こ/ま○こ/ま●こ/おま〇こ/おま○こ/おま●こ/おまんこ/まんこ/マンコ→보지; パイパンま〇こ/パイパンま○こ/パイパンま●こ/パイパンまんこ/無毛まんこ→백보지, 금지: 무모 소중이; alone パイパン→무모|백보지; ち〇ぽ/ち○ぽ/ち●ぽ/ちんこ/チンポ→자지; マン汁/本気マン汁→애액, 금지: 보짓물; ザーメン/ejaculation 精子→정액; レイプ/レ×プ/レ〇プ/レ○プ/レ●プ→강간, 금지: 레프; アナル→애널 for JAV act/genre, anatomical 肛門→항문; 初アナル→첫 애널; 初アナル解禁→첫 애널 해금, 금지: 첫 항문/첫 항문 해금; クンニ/クンニリングス→보빨, 금지: 쿤니; アクメ→절정|오르가슴, 금지: 아크메; デカチン/巨根→대물, 금지: 대물 자지/거대 자지/왕자지. Ex: パイパンま〇こから溢れ出る精子→백보지에서 흘러넘치는 정액; デカチン緩急ピストン→대물 완급 피스톤. 금지:소중이/그곳/중요 부위/여성의 신체.",
		"JAV titles: forceful noun phrases; no explanatory ~하는/~하게 되는/~을 조절하는 clauses.",
		"Source brackets: 【...】 stays 【...】, [...] stays [...]; never invent; bracketed 個撮→[개인촬영], never [POV].",
		"Trope: ご開帳→은밀한 부위 전체 공개; 手取り足取り→하나부터 열까지 직접 가르치는; 骨抜き→쾌감에 녹초가 된; 毒牙→위험한 유혹에 걸린; 生殺し→사정시키지 않고 애태우기.",
		"Terms: 股下→다리 길이; 美脚→각선미; 爆乳→폭유; 神乳→신의 가슴; 騎乗位→기승위; 背面騎乗位→후배위 기승위; デカ尻→큰 엉덩이; 尻コキ→엉덩이 성교; フェラ→펠라.",
	}
	return strings.Join(rules, " ") + " "
}

// translationCompactOutputMarker returns the compact output marker for the given index.
func translationCompactOutputMarker(i int) string {
	return fmt.Sprintf("<<<JZ_%d>>>", i)
}

// buildLLMTranslationResult parses the LLM response content into a translation result.
func buildLLMTranslationResult(content string, markerSpec any) (*translationResult, error) {
	parsed, err := parseLLMTranslationPayload(content, markerSpec)
	if err != nil {
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: err.Error(),
		}
	}
	return &translationResult{Texts: parsed, RawLLM: content}, nil
}

// decodeOpenAIChatTranslation decodes an OpenAI chat completion response into
// a translation result.
func decodeOpenAIChatTranslation(provider string, respBody []byte, markerSpec any, maxOutputTokens ...int) (*translationResult, error) {
	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", provider, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response contained no choices", provider)
	}

	content := extractContentString(decoded.Choices[0].Message.Content)
	finishReason := strings.ToLower(strings.TrimSpace(decoded.Choices[0].FinishReason))
	if finishReason == "length" || finishReason == "max_tokens" {
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: truncatedLLMOutputMessage(provider, content, finishReason, maxOutputTokens),
		}
	}
	complete, ok := stripLLMCompletionMarker(content)
	if !ok {
		// Older OpenAI integrations returned a JSON string array. Its closing
		// bracket is structurally self-terminating, so keep accepting that
		// legacy shape while requiring an explicit completion marker for the
		// current compact marker protocol.
		if legacy, legacyErr := parseStringArrayPayload(content); legacyErr == nil {
			return &translationResult{Texts: legacy, RawLLM: content}, nil
		}
		return &translationResult{RawLLM: content}, &translationError{
			Kind:    TranslationErrorParse,
			Message: truncatedLLMOutputMessage(provider, content, "missing completion marker "+llmCompletionMarker, maxOutputTokens),
		}
	}
	return buildLLMTranslationResult(complete, markerSpec)
}

func stripLLMCompletionMarker(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasSuffix(trimmed, llmCompletionMarker) {
		return content, false
	}
	return strings.TrimSpace(strings.TrimSuffix(trimmed, llmCompletionMarker)), true
}

func truncatedLLMOutputMessage(provider, content, reason string, maxOutputTokens []int) string {
	message := fmt.Sprintf(
		"%s translation output was truncated (%s; received_chars=%d",
		provider,
		reason,
		len([]rune(content)),
	)
	if len(maxOutputTokens) > 0 && maxOutputTokens[0] > 0 {
		message += fmt.Sprintf("; max_output_tokens=%d", maxOutputTokens[0])
	}
	return message + ")"
}

// executeLLMChatTranslation is the shared pipeline for LLM chat-based translation.
// It builds the request via the adapter, executes the HTTP call, and decodes the
// response via the adapter. This eliminates the duplicated prompt→execute→decode→parse
// logic across OpenAI and Anthropic providers.
func executeLLMChatTranslation(ctx context.Context, httpClient httpclient.HTTPClient, adapter LLMChatAdapter, providerName, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*translationResult, error) {
	req, err := adapter.BuildRequest(ctx, baseURL, model, systemPrompt, userPrompt, textCount)
	if err != nil {
		return nil, err
	}

	logging.Debugf("Translation (%s): POST %s model=%s texts=%d", providerName, req.URL, model, textCount)
	logging.Debugf("Translation (%s): system prompt: %s", providerName, systemPrompt)

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed after %v: %w", providerName, time.Since(start), err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s translation failed with status %d: %s", providerName, resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation (%s): response: %s", providerName, string(respBody))
	return adapter.DecodeResponse(providerName, respBody, textCount)
}

// executeOpenAIChatTranslation performs an OpenAI-compatible chat translation call
// using the legacy direct-request path (used by OpenAICompatibleProvider for
// thinking-strategy fallback).
func executeOpenAIChatTranslation(ctx context.Context, httpClient httpclient.HTTPClient, opts openAIChatCallOptions) (*translationResult, error) {
	body, err := json.Marshal(opts.request)
	if err != nil {
		return nil, err
	}

	url := opts.baseURL + opts.endpoint
	logging.Debugf("Translation (%s): POST %s model=%s texts=%d", opts.provider, url, opts.model, opts.textCount)
	logging.Debugf("Translation (%s): system prompt: %s", opts.provider, opts.request.Messages[0].Content)
	if opts.logInput && len(opts.request.Messages) > 1 {
		logging.Debugf("Translation (%s): input: %s", opts.provider, opts.request.Messages[1].Content)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range opts.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Time{}
	if opts.logTiming {
		logging.Debugf("Translation (%s): sending request...", opts.provider)
		start = time.Now()
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if opts.logTiming {
			return nil, fmt.Errorf("%s request failed after %v: %w", opts.provider, time.Since(start), err)
		}
		return nil, err
	}
	if opts.logTiming {
		logging.Debugf("Translation (%s): response received in %v (status %d)", opts.provider, time.Since(start), resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s translation failed with status %d: %s", opts.provider, resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation (%s): response: %s", opts.provider, string(respBody))
	markerSpec := any(opts.textCount)
	if len(opts.markers) > 0 {
		markerSpec = opts.markers
	}
	return decodeOpenAIChatTranslation(opts.provider, respBody, markerSpec, opts.request.MaxTokens)
}

// openAIChatAdapter implements LLMChatAdapter for OpenAI-compatible chat APIs.
type openAIChatAdapter struct {
	headers         map[string]string
	markers         []string
	maxOutputTokens int
}

func (a *openAIChatAdapter) BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error) {
	request := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   4096,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	url := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (a *openAIChatAdapter) DecodeResponse(providerName string, respBody []byte, textCount int) (*translationResult, error) {
	if len(a.markers) > 0 {
		return decodeOpenAIChatTranslation(providerName, respBody, a.markers, a.maxOutputTokens)
	}
	return decodeOpenAIChatTranslation(providerName, respBody, textCount, a.maxOutputTokens)
}

// extractContentString extracts a string value from a JSON RawMessage,
// falling back to the raw bytes if it's not a valid JSON string.
func extractContentString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
