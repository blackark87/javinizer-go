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

	terminologyRules := "General terminology rules: use established, current terminology from the target language's JAV/adult-video industry. Decode JAV idioms and production tropes by their actual industry meaning, not their literal dictionary image. Do not invent vocabulary. Ordinary nouns, verbs, compounds, idioms, concept names, and semantically transparent series names must be translated by meaning into concise natural target-language wording even when there is no one-word equivalent; never merely spell their Japanese pronunciation in the target script. Phonetic transliteration is a last resort reserved for person names, company or brand names, opaque coined proper nouns, and source terms genuinely used as loanwords in the target-language JAV industry. Translate every meaningful Latin, English, and Japanese portion; leave no Japanese script in non-Japanese output except protected person-name punctuation. "
	personNameRule := "Person-name rule: sections labeled <<<actress[N]>>> or <<<title_as_name>>> contain one performer's name. Transliterate phonetically, never translate the meaning, and keep Japanese order: FamilyName GivenName. Romaji input is the authoritative reading; do not re-derive it from kanji. Never invent, anglicize, or substitute a different performer name, including when a performer name appears at the end of a longer title. The Japanese middle dot ・ is punctuation inside one name: preserve it exactly, never turn it into a comma, and never split the name into multiple performers. For Korean, output Hangul and transliterate Hepburn syllables literally (Rena → 레나, Reina → 레이나; Yuu → 유, Tarou → 타로, Yui → 유이). Ignore age, cup size, height, and occupation extras; if no personal name remains, return an empty section. Also apply this rule to a short personal-name-like <<<title>>>. "
	properNounRule := "Proper-noun rule: <<<maker>>>, <<<label>>>, and <<<director>>> are names. Transliterate them phonetically and do not embellish them. "
	cleanupRules := "Title cleanup: remove bracketed VR/release labels such as [VR], 【VR】, and 【8K VR】. Description cleanup: remove playback/device notices, VR-only notices, platform notices, sales campaigns, and store promotions; if only excluded material remains, return an empty section. "
	placeholderRule := "Any Hangul already present is final and must be copied verbatim. Protected tokens of the form ⟦N⟧ must be reproduced exactly and never translated, removed, or renumbered. "
	koreanRules := koreanJAVPromptRules(targetLang)

	systemPrompt := fmt.Sprintf("You are an expert translator working strictly with Japanese adult video (JAV) metadata and AV-studio metadata. Use the concise, direct, contemporary vocabulary an actual AV production studio would publish. Never use corny, dated, literary, moralizing, broadcast-style euphemistic, or exaggerated erotic-advertising language. Titles and tags must be sharp marketing-ready metadata; descriptions must remain complete, natural modern prose rather than being reduced to keyword lists. Follow these rules: %s%s%s%s%s%sCRITICAL: return each translation under the same marker, never merge, omit, reorder, or swap sections. Return only markers and translated text; no JSON or commentary. Keep each translation on one logical line. Source language: %s. Target language: %s.", terminologyRules, koreanRules, personNameRule, properNounRule, cleanupRules, placeholderRule, sourceLang, targetLang)

	var userPrompt strings.Builder
	userPrompt.WriteString("Translate each labeled section below:\n")
	for i, text := range texts {
		userPrompt.WriteString(markers[i])
		userPrompt.WriteByte('\n')
		userPrompt.WriteString(text)
		userPrompt.WriteByte('\n')
	}
	userPrompt.WriteString("\nReturn output in the same labeled format:\n")
	for _, marker := range markers {
		userPrompt.WriteString(marker)
		userPrompt.WriteString("\n[translation]\n")
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func buildLLMQualityReviewPromptsWithMarkers(targetLang string, items []qualityReviewItem, markers []string) (string, string, error) {
	if len(items) == 0 || len(markers) != len(items) {
		return "", "", fmt.Errorf("quality review prompt requires one marker per item (%d markers for %d items)", len(markers), len(items))
	}
	systemPrompt := "You are the mandatory second-pass quality reviewer for strictly JAV/AV-studio metadata translated into Korean. Compare every Japanese source with its Korean candidate. Silently correct mistranslation, dictionary calques, Japanese slang transliteration, broken hybrid text, omitted meaning, invented meaning, awkward grammar, dated wording, and unnatural AV terminology. Preserve the source's explicitness and tone. Do not merely approve or critique the candidate: return the complete corrected final Korean text. Preserve protected tokens such as ⟦0⟧ exactly. Preserve correctly translated performer names and never replace them with a different performer. " + koreanJAVPromptRules(targetLang) + "CRITICAL: return exactly one corrected result under each original marker in the same order. Return only markers and corrected Korean text, with no assessment, explanation, JSON, or commentary."

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
	userPrompt.WriteString("\nReturn only the corrected final Korean text under the same markers.\n")
	for _, marker := range markers {
		userPrompt.WriteString(marker)
		userPrompt.WriteByte('\n')
	}
	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func koreanJAVPromptRules(targetLang string) string {
	lang := strings.ToLower(strings.TrimSpace(targetLang))
	if lang != "ko" && !strings.HasPrefix(lang, "ko-") && !strings.HasPrefix(lang, "ko_") {
		return ""
	}
	return "Korean JAV rules (non-negotiable priority): preserve the source's sexual explicitness exactly. Use direct current Korean AV vocabulary without sanitizing it, but never invent a sex act, insult, coercion, or stronger intensity absent from the source. Hard and explicit wording must still be fluent and cleanly written; it does not mean corny, food-like, or needlessly dirty phrasing. Prefer an established Korean AV term whenever one exists. Use a loanword only when no suitable Korean term exists or the source itself uses an established acronym such as POV, VR, or ASMR. " +
		"Write natural, concise, current Korean used by real AV studios. For meaningful expressions, prefer an established Korean AV term and otherwise use a concise semantic paraphrase. For opaque proper nouns or genuine industry loanwords only, prefer a widely used JAV loanword or acronym and otherwise use faithful Hangul transliteration. Never replace a missing equivalent with a literal calque, verbose definition, invented word, or phonetic spelling of an ordinary Japanese expression. For mappings with alternatives, choose exactly one that fits the context and never output alternatives joined by a slash, parentheses, or '또는'. Strict contextual mappings: " +
		"数珠つなぎ → 릴레이 or 연속; たすきリレー and バトンリレー → 바통 터치 or 릴레이; 芋づる式 → 연쇄 or 연속; ハシゴ酒 → 술집 투어 or 술집 순례; 朝までハシゴ酒 → 밤새 술집 투어 (never 아침까지 하시고주); " +
		"パパ活 and Sugar Dating → 스폰 or 조건; 一本釣り → 독점 스카우트 or 길거리 캐스팅; 箱入り and 箱入り娘 → 아가씨 or 순진녀; 逆指名 → 여배우의 선택 or 역지명; " +
		"垢抜け → 비주얼 업그레이드 or 세련된; 初々しい → 풋풋한 or 앳된; 玄人 and 玄人肌 → 프로 or 능숙한; " +
		"中出し and Creampie → 질내사정; 顔射 and Facial → 안면사정; ぶっかけ and Bukkake → 정액 세례 or 붓카케; 個撮 → 개인촬영; ハメ撮り → POV or 셀프카메라; POV may remain POV when natural; 汁男優 → 사정 전문 남배우. " +
		"Established Korean JAV mappings: 顔騎 → 안면기승 (never 페이스시팅 or 얼굴 기승위); 足裏 → 발바닥; 足コキ → 풋잡 (never 발코키); in pantyhose fetish context ムレた足裏 → 땀에 찬 발바닥 or 땀이 밴 발바닥 (never 눅눅한 발바닥); 足裏からつま先を味わい尽くす → 발바닥부터 발끝까지 샅샅이 맛본다. " +
		"桃尻 and the mixed-script stylization 桃Siri describe a peach-shaped butt and must be translated as 애플힙, never treated as a person name and never transliterated as 모모시리. " +
		"ミルチオ is the opaque JAV coinage built from performer miru and フェラチオ: transliterate it as 미루치오, never 밀치오. ミルチオの愛人 means a mistress who performs the technique, so translate it as 미루치오를 해주는 불륜 상대, never the nonsensical possessive 미루치오의 정부. In an adultery context 愛人 → 불륜 상대, never the ambiguous or stiff 정부 and never the generic 애인. 絶倫性欲者 referring to a woman → 절륜 색녀, and referring to a man → 절륜남; never 절륜 성욕자. " +
		"In penile stimulation context シゴく and シゴき mean stroking or jerking by hand, not a loanword: use 손으로 흔들다, 핸드잡, or 뽑아주다 according to grammar. 舐めシゴき → 혀와 손으로 뽑아주기 or 핥기와 핸드잡; 舐めシゴきフルコース → 혀와 손으로 뽑아주는 풀코스. Never write 시고키, 핥기 시고키, or 사랑의 핥기. In compounds such as 愛ベロ and 愛舐め, 愛 intensifies intimate technique and must not mechanically become the corny phrase 사랑의. " +
		"エビ反り is the orgasm posture where the waist arches: always use 허리가 휘는. エビ反り痙攣絶頂 → 허리가 휘며 경련 절정; エビ反りオーガズム → 허리가 휘는 오르가슴. Never write 허리 꺾인, 새우등처럼 휜, 등을 활처럼 휘는, 허리를 뒤로 젖히는, 에비반리, or 에비소리. In erotic massage or esthetic titles, 特別施術 → 특별 코스, never the clinical 특별 시술 or 특별 트리트먼트. " +
		"人妻 → 유부녀 and 人妻もの → 유부녀물. Never use the dated Japanese-calque term 인처. " +
		"セフレ means 섹스 파트너 or the current colloquial abbreviation 섹파 according to tone; never transliterate it as 세프레. セフレ志願の女の子 → 섹파를 자처하는 여자 or 섹스 파트너를 원하는 여자. In playful sexual phrasing, ちんちんおっきしたら → 자지가 서면; never produce broken hybrids such as n자지 커지면. " +
		"In sexual context 寸止め means stopping or teasing just before climax: use 절정 직전 멈추기, 절정 직전까지 애태우기, or 절정을 참게 하는 according to grammar, never the borrowed English calque 에징. リモバイ abbreviates a remote-controlled vibrator and must be rendered as 리모트 바이브, never transliterated as 리모바이. 寸止めリモバイ調教 → 리모트 바이브로 절정 직전까지 애태우는 조교. 小鹿アクメ is a visual trope for an orgasm that leaves the knees and legs trembling like a newborn fawn: render the bodily result naturally as 다리가 후들거리는 절정 or 무릎이 풀리는 절정, never 새끼 사슴 오르가슴. 膝ガクガク小鹿アクメ → 무릎이 후들거리는 절정. " +
		"秘部 is a source-side euphemism: render it naturally as 은밀한 곳 or 민감한 부위 according to the sentence, never the dictionary calque 비부. きわどい秘部を触られすぎて → 은밀한 곳을 집요하게 만져져, not 아슬아슬한 비부를 너무 많이 만져져서. Do not mechanically transliterate 寝取られました as 네토라레 당했습니다 in prose; express the event naturally from the subject's perspective, such as 다른 남자에게 넘어가 버렸다. Keep NTR only when it functions as a concise genre label. " +
		"The emphatic prefix ド attached to a sexual trait intensifies the trait and must never be transliterated as 도. ド痴女 → 극강의 치녀 or 지독한 색녀 according to tone, never 도치녀; ド変態 → 극도의 변태 or 지독한 변태. 結婚した妻 should normally be the concise 아내, not the redundant 결혼한 아내. 性欲おさまらない → 멈출 줄 모르는 성욕 or 주체할 수 없는 성욕. " +
		"言いなり and イイナリ mean obeying another person's demands: use 시키는 대로 하는, 말이라면 뭐든 따르는, or 복종하는 according to grammar, never transliterate them as 이이나리. ドM means an extreme masochist or strongly submissive M: use 극M or 극도의 마조 according to tone, never 도M. イイナリドM → 말이라면 뭐든 따르는 극M or 복종하는 극M. " +
		"逆パコ is a woman initiating, taking control of, or pouncing on a man for sex: use 여자가 덮치는, 여배우가 덮치는, or 여자 주도 섹스 according to grammar; never transliterate it as 역파코. 痴女られる means being sexually toyed with or dominated by an assertive woman: がっつり痴女られたい → 치녀에게 실컷 농락당하고 싶다, never 듬뿍 치녀 취급당하고 싶다. " +
		"パコ, パコる, and パコパコ are ordinary Japanese sex slang, not Korean loanwords: translate them by context as 섹스, 섹스하다, or 박아대다; never transliterate them as 파코. イキパコ means sex involving climax: use 절정 섹스, 가버리는 섹스, or a fluent contextual equivalent, never 이키파코 or 파코. " +
		"Render パコ compounds by their actual AV-marketing meaning: オフパコ → 비밀 만남 섹스 or 팬과의 섹스 according to context, 生パコ → 노콘 섹스, イチャパコ → 달달한 섹스, and パコパコ撮影 → 마구 섹스하는 촬영. Do not retain 파코 inside a compound. " +
		"ポルチオ in JAV copy denotes deep vaginal stimulation. In polished titles and descriptions use 깊숙한 피스톤, 질 깊숙이 파고드는 피스톤, or 질 깊은 곳을 자극하다 according to grammar and source intensity. Do not exaggerate it as 강타하다 or 집중 공략하다. Never use the clinical 자궁경부, and never write the conversational calque 질 안쪽을 찌르다. " +
		"ジュボジュボ is a wet action sound, not a Korean loanword. Express the depicted action: for sucking a penis use 자지를 질척하게 빨아대다; for licking a penis use 자지를 침 범벅으로 핥아대다; for licking body parts use 축축하게 핥아대다. Never output 쥬보쥬보. In aggressive narrative 1発ハメる can be 한 번 따먹다; never use the awkward 한 판 박아버리다. " +
		"Never transliterate Japanese sexual sound-symbolic words into Hangul. Translate the visible action or result instead: ドピュドピュ → 연속 사정 or 정액을 연달아 뿜다; じゅぽじゅぽ, じゅっぽんじゅっぽん, グポグポ, and ジュルル → 질척하게 빨아대다 or 입 깊숙이 삼켜 빨아대다; ズボズボ → 깊숙이 박히는 피스톤; チュパチュパ and ペロちゅぱ → 진하게 빨아대다 or 핥고 빨아대다. Never output 도퓨도퓨, 쥬폰쥬폰, 쥬퓻쥬퓻, 쥬포쥬포, 쥬르르, 즈보즈보, 츄파츄파, 페로츄파, or 츄릅츄릅. " +
		"In explicit multi-orifice JAV titles, a numeral followed by 穴 counts sexual orifices: use 홀 in compressed headlines or 구멍 in prose, never the Hanja reading 혈, and never leave 穴 untranslated. Thus 3穴 → 3홀 or 세 구멍, and 2穴セフレ → 2홀 섹파 or 두 구멍을 내주는 섹파 according to grammar. ごっくん in a semen context means swallowing semen: use 정액 삼키기 or 정액을 삼키다, never transliterate it as 고쿤. ノドマンコ describes the throat as an orifice: use 목구멍, and ケツマンコ means 후장; never write 목구멍 보지 or 똥보지. " +
		"ストゼロ is the alcoholic drink brand Strong Zero: write 스트롱 제로, never 스트로제로 or 스트로 제로. 潮吹き means squirting sexual fluid: use 분수, 애액 분출, or 애액을 뿜다 according to grammar, never 스포팅 or a phonetic Japanese spelling. Reconstruct compounds compositionally: 限界ストゼロ潮吹きFUCK → 스트롱 제로를 마시며 한계까지 분수를 뿜는 섹스. " +
		"Translate Japanese mimetic slang by meaning: イクイク → 연속 절정 or 계속 가버리는; プリプリ尻 → 탱탱한 엉덩이; デレデレ → 푹 빠진 or 애정 가득한; エロエロ → 음란한. In sexual context チンしゃぶ means 펠라 or 자지를 핥고 빨다, never the food term 자지 샤브샤브. " +
		"In JAV sexual context おしゃぶり means 펠라 or 자지 빨기, not 공갈 젖꼭지. 吸引おしゃぶり → 빨아들이는 펠라 or 강하게 빨아대는 펠라, never 흡입 오샤부리. Preserve an actual pacifier meaning only when the surrounding scene explicitly concerns a baby pacifier. " +
		"鉄マン is explicit JAV slang for an exceptionally strong or tight vagina: translate it as 강철 보지, never transliterate it as 철맨. In punchy title copy マジかよ！？ → 실화냐?! or 말도 안 돼?! according to tone; 秘技教本 → 비법 교본, never the stiff calque 비기 교본. " +
		"生ハメ → 노콘 (never 생하메, 생삽입, or 생으로 하메); 生ハメSEX → 노콘 섹스; 生ハメ中出し → 노콘 질내사정. シコサポ, オナサポ, and オナニーサポート → 자위 서포트 (never 시코사포 or 오나사포). " +
		"媚薬 → 최음제 (never 피임약). キメセク always describes sex while chemically intoxicated: with 媚薬 use 최음제에 취한 섹스 or 최음제 섹스, and with narcotics, stimulants, or unspecified drugs use 약에 취한 섹스 or 약물 섹스. Never output 키메섹; never 킴세쿠 or 키메세쿠, even when it appears as a standalone genre term. キメセクの巣 → 약물 섹스의 소굴. In an explicit drug context ガンギマリ means the drug has taken full effect: use 약에 완전히 취한 or 약기운이 제대로 오른, never the vague 완전히 맛이 간. " +
		"タイパ abbreviates time performance and means efficiency relative to time spent: use 시간 효율 or 시간 대비 효율, never transliterate it as 타이파. タイパを気にし過ぎる → 시간 효율을 지나치게 따지는. フェザータッチ and フェザー describe a barely touching, feather-light caress. In penile stimulation, フェザー指コキ means gently teasing or stroking the penis with the fingertips: use 깃털처럼 살살 애태우는 손가락 자위 or 손가락으로 살살 흔들어주기, never 페더 손가락 핸드잡, 페더 지코키, or the redundant 손가락 핸드잡. In ejaculation context 暴発 means losing control and ejaculating: use 참지 못하고 사정하다 or 터뜨리다, never the literal 폭발 or 폭발 교육. " +
		"When 水浸し appears in erotic hotel, bed, or body context without real flooding, it exaggerates wetness from sexual fluids: use 침대가 흠뻑 젖도록, 러브호텔을 흠뻑 적시며, or 애액으로 흠뻑 젖은 according to grammar; never write 러브호텔 물바다 unless actual water or flooding is described. " +
		"ヤリマン is a derogatory description of a sexually promiscuous woman: use 문란녀, 헤픈 여자, or 아무하고나 자는 여자 according to tone, never transliterate it as 야리만. 逆ナン means a woman approaching or picking up a man: use 여자가 남자를 헌팅하는 or 남자 사냥 according to grammar, never 역나 or 역난. 逆ナンドライブ → 남자를 헌팅하는 드라이브 or 남자 사냥 드라이브. In compressed AV titles 淫行 usually denotes the depicted sexual activity: translate it naturally as 섹스 or fold it into a phrase such as 남자를 꼬셔 섹스하는, never default to the stiff legal calque 음행 unless the source clearly invokes legal misconduct. 甘サド means sweet or gentle sadistic teasing: use 달콤하게 괴롭히는 S or 상냥한 S플레이, never 달콤 사디스틱. 杭打ちピストン describes forceful vertical thrusting: use 위에서 거칠게 내리꽂는 피스톤 or 찍어 누르는 피스톤 according to the scene, never 말뚝박기 피스톤. Do not confuse it with the established position name 杭打ち騎乗位 → 말뚝박기 기승위. " +
		"しろーと and 素人 describe an amateur or non-professional: use 아마추어 or 일반인 according to context, never transliterate them as 시로토 or 시로트. 逆ナンパ has the same female-pickup meaning as 逆ナン: use 여자가 남자를 헌팅하다 or 남자 사냥, never 역지명. キャバ嬢 means a hostess working at a cabaret club: use 캬바걸 or 캬바클럽 호스티스, never transliterate it as 캬바죠 or 카바죠. 美乳 and 超美乳 describe attractive breasts: use 예쁜 가슴, 아름다운 가슴, or 매우 아름다운 가슴, never 미유 or 초미유. Always spell インフルエンサー as 인플루언서, never 인플루큐언서. In body-description context スタイル and 体型 or the typo 体系 mean 몸매 or 체형, not 스타일 or 체계: スタイル最強 and 最強スタイル → 최강 몸매; 理想のモテ体型 and 理想のモテ体系 → 이상적인 인기 몸매. 極上 used as praise → 최고 or 최상급, never 극상; エロい → 야한, never 에로한; 極エロ → 극도로 야한 or 극강의 야함, never 극에로. びんびんフル勃起 → 빳빳하게 완전 발기, never the redundant 풀 발기. " +
		"寝取り is the act of taking or seducing someone else's partner, whereas 寝取られ is having one's partner taken; preserve that direction and never turn 寝取り into 네토라레. ちんぐり返し depicts a man on his back with both legs forced toward his head and his hips and anus raised and exposed. Do not name the pose or add domination and violence absent from the source; reconstruct the performed act from the compound in concise promotional wording. ちんぐり返し騎乗位 → 남자의 다리를 뒤로 젖힌 기승위; ちんぐり返しアナル舐め → 남자의 다리를 뒤로 젖혀 항문 핥기. Never transliterate it as 친구리, 친구리카에시, or 치무가에리, and never use shape metaphors such as 새우, 쟁기, or 활. タイマン means a one-on-one showdown: use 일대일 맞대결 or 1대1 승부, never 타이만. タイマン4本番 → 1대1 맞대결 본방 4회. " +
		"胸糞 expresses disgust or a sickening feeling: 胸糞NTR → 역겨운 NTR or 기분 더러운 NTR, never leave 胸糞 untranslated. 鬱勃起 is a deliberately bleak erection trope: use 우울한데도 발기되는 or 우울 발기 according to title grammar, never 울울한 발기. When 壊される describes a person in NTR copy, preserve the emotional destruction with 망가지다 or 망가뜨리다; do not omit it. " +
		"In explicit sexual prose 蜜壺 is a metaphor for the vagina: translate it naturally as 보지 or 질 according to grammar, never use the dictionary calques 밀통, 꿀단지, or 비부. 手マン performed by another person means 핑거링 or 손가락으로 보지를 자극하다, never 손가락 자위. 美意識溢れる体 describes a carefully beautified body: use 아름답게 가꾼 몸, not the literal 미적 감각이 넘치는 몸. In a comma-separated keyword list, 浅草 is the place name 아사쿠사, while ordinary fortune-result 大吉 means 대길 or 대박; use 다이키치 only when context establishes it as a person's name. 玩具責め in promotional headlines means 성인용품 공세 or 장난감 조교 according to tone, never 장난감 괴롭히기 or 장난감 괴롭힘. 確定ビッチ describes an unmistakably promiscuous woman: use 확실한 문란녀, never 확정 비치. When 大・連・発 or 大連発 describes a woman's climax, use 연속 절정, never 연속 사정 and never leave it as 대·연·발. ヤリモクインフルエンサー → 섹스만 노리는 인플루언서, never 섹스 목적인 인플루언서. " +
		"For POV marketing 完全主観 means 완전 1인칭 시점, never 완전 주관. 青春グラフィティ means 청춘 이야기 or 청춘 기록, never 청춘 그래피티. とびきりエッチ → 아주 야한 or 유난히 야한, not 아주 특별한. Preserve the deliberate 青春/性春 wordplay as 청춘/성춘 when both occur. 女優の本音と女優の本気 → 여배우의 솔직한 속마음과 진짜 모습, never 진심과 진지함. " +
		"When 沼 is a slang suffix for an irresistible fixation, express the attraction as 푹 빠지는 or 헤어나올 수 없는 according to grammar, never use the literal 늪. Thus ビッチ沼 means 문란녀에게 푹 빠지는 or 헤어나올 수 없는 문란녀의 매력. In entertainment or idol context 枕営業 means 성상납, never leave it in Japanese and never blur it into the broader 스폰. 色白 describes pale or fair skin: use 하얀 피부 or 피부가 하얀, never the dictionary calque 색백. 僕の身代わりに means 나 대신, never mechanically render の as possessive 내. バクヌキ means intensely or repeatedly getting someone off: use 실컷 빼주는 or 마구 뽑아주는 according to grammar, never transliterate it as 바쿠누키. 挟射 means ejaculation while held between the breasts: use 가슴 사이에 끼워 사정. In a sexually disparaging compound よわよわ means pathetic or hopeless rather than physically weak: おま○こよわよわ → 보지 허접, never 보지 약한. " +
		"Interpret insults by their target, not their dictionary homograph: fish-context 雑魚 → 잡어, but a sexually belittling 雑魚 → 허접, 하찮은, or 찌질한, and 雑魚チ●ポ → 허접 자지 (never 잡어 자지). Food-context 食い意地 → 식탐, but when 喰い意地爆発 governs 肉棒, 巨根, or sex acts use 욕정 폭발 (never 식탐 폭발). " +
		"Do not force context-sensitive expressive verbs into one fixed Korean phrase. In sexual prose, choose a fluent rendering for むしゃぶりつく from its object and sentence, and never use the food-like adverb 게걸스럽게. For ASMR compounds, reconstruct the performed act and sound instead of stacking dictionary nouns: ベチョレロ唾液チ〇ポ咀嚼 means 타액 범벅 펠라 소리, and ヌチュグチュ粘着マン汁音 means 끈적한 애액이 질척이는 소리; never write 자지 저작 or invent a trailing 섹스! absent from the source. " +
		"Preserve person honorifics in translated titles and descriptions when attached to a name: さん and 氏 → 씨; 様 → 님; ちゃん and たん → 짱; くん and 君 → 군. Thus みあたん → 미아짱, never 미아탄, 미아상, or 미아사마. Do not mistake a grammatical word such as 様 meaning 모습 for a person honorific. " +
		"Do not sanitize explicit anatomy or sex acts. When the source uses an obscene, explicit, or partially censored genital term, restore its meaning and translate it at the same explicitness with current Korean AV vocabulary; never phonetically spell the censored fragments. Never replace it with coy euphemisms such as 소중이, 그곳, 은밀한 부위, 중요 부위, or 여성의 신체. Strict explicit mappings: ま〇こ, ま○こ, ま●こ, おま〇こ, おま○こ, おま●こ, おまんこ, まんこ, and マンコ → 보지; パイパンま〇こ, パイパンま○こ, パイパンま●こ, パイパンまんこ, and 無毛まんこ → 백보지 (never 무모 소중이); パイパン by itself → 무모 or 백보지 according to grammar; ち〇ぽ, ち○ぽ, ち●ぽ, ちんこ, and チンポ → 자지; マン汁 and 本気マン汁 → 애액 (never 보짓물); ザーメン and 精子 in ejaculation context → 정액; レイプ, レ×プ, レ〇プ, レ○プ, and レ●プ → 강간 (never 레프); クンニ and クンニリングス → 보빨 in explicit JAV titles, never 쿤니; アクメ → 절정 or 오르가슴 according to grammar, never 아크메; デカチン and 巨根 → 대물, never 대물 자지, 거대 자지, or 왕자지. Examples: パイパンま〇こから溢れ出る精子 → 백보지에서 흘러넘치는 정액; デカチン緩急ピストン → 대물 완급 피스톤. " +
		"Keep the compressed headline style of JAV titles. Prefer short, forceful noun phrases and stacked marketing terms; do not expand them into explanatory conversational clauses with added forms such as '~하는', '~하게 되는', or '~을 조절하는'. " +
		"Square or corner brackets are source punctuation, not metadata tags: preserve them only when they exist in the source and never invent a new bracketed tag. Preserve the exact bracket glyph style one-for-one: 【...】 must remain 【...】 and may not become [...], while [...] must remain [...]. In particular, bracketed 個撮 must become [개인촬영], never [POV]. " +
		"Interpret these JAV tropes by intent, using current Korean AV wording rather than literal imagery: ご開帳 means the full intimate reveal or unveiling; 手取り足取り means hands-on step-by-step intimate guidance; 骨抜き means being left weak from intense pleasure; 毒牙 means falling prey to predatory or corrupting seduction; 生殺し means edging or teasing without release. " +
		"Additional established Korean terminology: 股下 → 다리 길이; 美脚 → 각선미; 爆乳 → 폭유; 神乳 → 신의 가슴; 騎乗位 → 기승위; 背面騎乗位 → 후배위 기승위; 杭打ち騎乗位 → 말뚝박기 기승위; デカ尻 → 큰 엉덩이; 尻コキ → 엉덩이 성교; フェラ → 펠라. "
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
func decodeOpenAIChatTranslation(provider string, respBody []byte, markerSpec any) (*translationResult, error) {
	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", provider, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response contained no choices", provider)
	}

	return buildLLMTranslationResult(extractContentString(decoded.Choices[0].Message.Content), markerSpec)
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
	return decodeOpenAIChatTranslation(opts.provider, respBody, markerSpec)
}

// openAIChatAdapter implements LLMChatAdapter for OpenAI-compatible chat APIs.
type openAIChatAdapter struct {
	headers map[string]string
	markers []string
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
		return decodeOpenAIChatTranslation(providerName, respBody, a.markers)
	}
	return decodeOpenAIChatTranslation(providerName, respBody, textCount)
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
