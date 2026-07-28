package translation

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"
)

var ageOccupationSuffixRE = regexp.MustCompile(`\s+[0-9０-９]+歳.*`)

var actressCommaSeparatorRE = regexp.MustCompile(`\s*[,，、]\s*`)

var actressNameHonorifics = []string{"ちゃん", "くん", "さん", "様", "氏", "君"}

var nihonshikiToHepburn = strings.NewReplacer(
	"sya", "sha", "syu", "shu", "syo", "sho",
	"tya", "cha", "tyu", "chu", "tyo", "cho",
	"zya", "ja", "zyu", "ju", "zyo", "jo",
	"si", "shi", "ti", "chi", "tu", "tsu", "zi", "ji", "hu", "fu",
)

// CleanActressName removes scraper-added age, occupation, honorific and
// promotional text, leaving only the performer name used for translation and
// identity matching.
func CleanActressName(name string) string { return cleanActressNameForTranslation(name) }

// CleanActressInfo normalizes a scraper actress and reports whether it changed.
func CleanActressInfo(info *models.ActressInfo) bool {
	if info == nil {
		return false
	}
	before := *info
	info.JapaneseName = cleanActressNameForTranslation(info.JapaneseName)
	info.FirstName = cleanActressNameForTranslation(info.FirstName)
	info.LastName = cleanActressNameForTranslation(info.LastName)
	if models.IsDescriptiveNonName(info.LastName, info.FirstName, info.JapaneseName) {
		info.FirstName = models.UnknownActressName
		info.LastName = ""
		info.JapaneseName = models.UnknownActressName
		info.ThumbURL = ""
	} else {
		models.CanonicalizeUnknownActressInfo(info)
	}
	return before.FirstName != info.FirstName || before.LastName != info.LastName ||
		before.JapaneseName != info.JapaneseName || before.ThumbURL != info.ThumbURL
}

// CleanStoredActress normalizes a persisted actress and reports whether it changed.
func CleanStoredActress(actress *models.Actress) bool {
	if actress == nil {
		return false
	}
	before := *actress
	actress.JapaneseName = cleanActressNameForTranslation(actress.JapaneseName)
	if before.JapaneseName != actress.JapaneseName {
		actress.FirstName, actress.LastName = "", ""
	}
	actress.FirstName = cleanActressNameForTranslation(actress.FirstName)
	actress.LastName = cleanActressNameForTranslation(actress.LastName)
	if models.IsDescriptiveNonName(actress.LastName, actress.FirstName, actress.JapaneseName) {
		actress.FirstName = models.UnknownActressName
		actress.LastName = ""
		actress.JapaneseName = models.UnknownActressName
		actress.ThumbURL = ""
	} else {
		models.CanonicalizeUnknownActress(actress)
	}
	return before.FirstName != actress.FirstName || before.LastName != actress.LastName ||
		before.JapaneseName != actress.JapaneseName || before.ThumbURL != actress.ThumbURL
}

func cleanActressNameForTranslation(name string) string {
	name = strings.TrimSpace(name)
	for _, paren := range []string{"（", "("} {
		if index := strings.Index(name, paren); index >= 0 {
			name = strings.TrimSpace(name[:index])
		}
	}
	if strings.HasPrefix(name, "[") {
		if end := strings.LastIndex(name, "]"); end > 0 {
			name = strings.TrimSpace(name[1:end])
		}
	}
	if idx := strings.Index(name, ","); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	name = trimMiddleDotDescriptorSuffix(name)
	name = strings.TrimSpace(ageOccupationSuffixRE.ReplaceAllString(name, ""))
	if honorificName, ok := extractHonorificNameToken(name); ok {
		name = honorificName
	}
	if containsResidualJapanese(name) {
		if tokens := strings.Fields(name); len(tokens) > 1 {
			kept := make([]string, 0, len(tokens))
			for _, token := range tokens {
				if !models.ContainsDescriptorKeyword(token) {
					kept = append(kept, token)
				}
			}
			if len(kept) > 0 {
				name = strings.Join(kept, " ")
			}
		}
	}
	for _, suffix := range actressNameHonorifics {
		if strings.HasSuffix(name, suffix) && len([]rune(name)) > len([]rune(suffix)) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	return strings.TrimSpace(name)
}

// trimMiddleDotDescriptorSuffix preserves middle dots inside real performer
// names while still dropping scraper-added attributes such as "・Hカップ".
func trimMiddleDotDescriptorSuffix(name string) string {
	parts := strings.Split(name, "・")
	if len(parts) < 2 {
		return name
	}
	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if models.ContainsDescriptorKeyword(part) {
			return strings.TrimSpace(strings.Join(parts[:i], "・"))
		}
	}
	return name
}

// restoreActressMiddleDots repairs the common LLM failure where an intrinsic
// Japanese middle dot in one performer name is rendered as a list comma.
func restoreActressMiddleDots(source, translated string) string {
	want := strings.Count(source, "・")
	if want == 0 || strings.Count(translated, "・") == want {
		return translated
	}
	restored := actressCommaSeparatorRE.ReplaceAllString(translated, "・")
	if strings.Count(restored, "・") == want {
		return restored
	}
	return translated
}

// flexibleJapaneseNamePattern matches one Japanese actress name while allowing
// scraper/source differences in ASCII or full-width whitespace between its
// characters. The characters themselves remain exact, so ordinary prose is
// not normalized or matched loosely.
func flexibleJapaneseNamePattern(name string) *regexp.Regexp {
	compact := strings.Join(strings.Fields(strings.TrimSpace(name)), "")
	if compact == "" {
		return nil
	}
	var pattern strings.Builder
	for index, r := range []rune(compact) {
		if index > 0 {
			pattern.WriteString(`[\s　]*`)
		}
		pattern.WriteString(regexp.QuoteMeta(string(r)))
	}
	return regexp.MustCompile(pattern.String())
}

// extractHonorificNameToken finds a name token carrying a Japanese honorific
// even when scraper-added occupation text follows it. Descriptor-only tokens
// such as "奥さん" are ignored so they cannot become a false performer name.
func extractHonorificNameToken(name string) (string, bool) {
	if tokens := strings.Fields(name); len(tokens) > 1 {
		for _, token := range tokens {
			if models.ContainsDescriptorKeyword(token) {
				continue
			}
			for _, suffix := range actressNameHonorifics {
				if !strings.HasSuffix(token, suffix) {
					continue
				}
				bare := strings.TrimSpace(strings.TrimSuffix(token, suffix))
				if bare != "" && !models.IsDescriptiveNonName("", "", bare) {
					return bare, true
				}
			}
		}
	}
	return "", false
}

var descriptionPromoStoreRE = regexp.MustCompile(`(?:特集\s*)?最新作やセール商品など、お得な情報満載[の의]\s*『[^』]*KMPストア[^』]*』はこちら！?`)
var descriptionFANZABonusPrefixRE = regexp.MustCompile(`^特典・セット商品イメージ\s*特典・セット商品情報\s*【特典内容】.*?特典付き商品・セット商品について`)
var descriptionReleaseDatePrefixRE = regexp.MustCompile(`^[\s　]*[【\[][\s　]*(?:発売日|발매일)[\s　]*[】\]][\s　]*[^,，]+[,，][\s　]*`)
var descriptionRuntimePrefixRE = regexp.MustCompile(`^[\s　]*[【\[][\s　]*(?:収録時間|수록[\s　]*시간)[\s　]*[】\]][\s　]*[^,，]+[,，][\s　]*`)
var descriptionContentIDPrefixRE = regexp.MustCompile(`^[\s　]*\([A-Za-z0-9][A-Za-z0-9_-]*\)[\s　]*`)

var descriptionPromotionalAnchors = []string{
	"※この作品はバイノーラル録音されております", "※ この作品はバイノーラル録音されております",
	"※この商品は専用プレイヤーでの視聴に最適化されています", "※ この商品は専用プレイヤーでの視聴に最適化されています",
	"※VR専用作品は必ず下記リンクより動作環境・対応デバイス", "※ VR専用作品は必ず下記リンクより動作環境・対応デバイス",
	"「動作環境・対応デバイス」について", "※ 配信方法によって収録内容が異なる場合があります",
	"※配信方法によって収録内容が異なる場合があります", "特集 最新作やセール商品など、お得な情報満載",
	"最新作やセール商品など、お得な情報満載",
	"※こちらはBlu-ray Disc専用ソフトです", "※ こちらはBlu-ray Disc専用ソフトです",
}

func cleanDescriptionForTranslation(description string) string {
	description = strings.TrimSpace(description)
	description = descriptionFANZABonusPrefixRE.ReplaceAllString(description, "")
	beforeMetadata := description
	description = descriptionReleaseDatePrefixRE.ReplaceAllString(description, "")
	description = descriptionRuntimePrefixRE.ReplaceAllString(description, "")
	if description != beforeMetadata {
		description = descriptionContentIDPrefixRE.ReplaceAllString(description, "")
	}
	cutAt := len(description)
	for _, anchor := range descriptionPromotionalAnchors {
		if index := strings.Index(description, anchor); index >= 0 && index < cutAt {
			cutAt = index
		}
	}
	description = strings.TrimSpace(description[:cutAt])
	description = descriptionPromoStoreRE.ReplaceAllString(description, "")
	description = asciiSpaceRunRE.ReplaceAllString(description, " ")
	return strings.TrimSpace(description)
}

var vrMarkerRE = regexp.MustCompile(`[\[【［(（][\s　]*(?:\d+[\s　]*[KkＫｋ][\s　]*)?[VvＶｖ][RrＲｒ](?:[\s　]*(?:専用|動画|作品))?[\s　]*[\]】］)）]`)
var promoMarkerRE = regexp.MustCompile(`[\[【［(（][^\]】］)）]*(?:限定|特典|セール|キャンペーン|独占|割引)[^\]】］)）]*[\]】］)）]`)
var titleDevicePromoSuffixRE = regexp.MustCompile(`[ \t　]*(?:[（(]ブルーレイディスク[）)](?:[ \t　]*生写真[0-9０-９]+枚付き)?|生写真[0-9０-９]+枚付き)[ \t　]*$`)
var titleSourceAttributionSuffixRE = regexp.MustCompile(`[ \t　]*(?:：|:)[^：:]{0,200}(?:MGS動画|Mgs動画|アダルト動画配信サイト|プレステージ[ \t　]*グループ)[^：:]*$`)
var asciiSpaceRunRE = regexp.MustCompile(`[ \t]{2,}`)
var bracketedPrivateShootRE = regexp.MustCompile(`[\[【［][\s　]*(?:個撮|個人撮影)[\s　]*[\]】］]`)
var bracketedKoreanPrivateShootRE = regexp.MustCompile(`[\[【［][\s　]*개인\s*촬영[\s　]*[\]】］]`)
var bracketedPOVRE = regexp.MustCompile(`(?i)[\[【［][\s　]*POV[\s　]*[\]】］]`)

func cleanTitleForTranslation(title string) string {
	title = stripPromoMarkers(stripVRMarkers(title))
	title = titleSourceAttributionSuffixRE.ReplaceAllString(title, "")
	return strings.TrimSpace(titleDevicePromoSuffixRE.ReplaceAllString(title, ""))
}

// prepareTitleForTranslation protects Korean title terminology whose source
// punctuation is semantically significant. LLMs otherwise tend to conflate
// bracketed 個撮 with the metadata-looking tag [POV].
func prepareTitleForTranslation(title, targetLang string) string {
	title = cleanTitleForTranslation(title)
	if normalizeLanguage(targetLang) == "ko" {
		title = bracketedPrivateShootRE.ReplaceAllString(title, "[개인촬영]")
	}
	return title
}

// finalizeTitleTranslation enforces the source-aware half of the rule: only a
// source bracketed as 個撮 owns the [개인촬영] marker. Literal source [POV]
// remains untouched, while an LLM-generated [POV] for 個撮 is corrected.
func finalizeTitleTranslation(source, translated, targetLang string) string {
	translated = strings.TrimSpace(translated)
	if normalizeLanguage(targetLang) != "ko" || !strings.Contains(source, "[개인촬영]") {
		if normalizeLanguage(targetLang) == "ko" {
			return normalizeKoreanTitleSeparators(translated)
		}
		return translated
	}
	translated = bracketedPOVRE.ReplaceAllString(translated, "[개인촬영]")
	translated = bracketedKoreanPrivateShootRE.ReplaceAllString(translated, "[개인촬영]")
	if !strings.Contains(translated, "[개인촬영]") {
		translated = strings.TrimSpace("[개인촬영] " + translated)
	}
	return normalizeKoreanTitleSeparators(translated)
}

var japaneseTitleSeparatorRE = regexp.MustCompile(`[ \t]*ー[ \t]*`)
var koreanCountRuntimeSeparatorRE = regexp.MustCompile(`([0-9]+명)[ \t]*,?[ \t]*([0-9]+분)`)

func normalizeKoreanTitleSeparators(value string) string {
	value = japaneseTitleSeparatorRE.ReplaceAllString(value, " - ")
	value = koreanCountRuntimeSeparatorRE.ReplaceAllString(value, "$1, $2")
	return strings.TrimSpace(value)
}

func normalizeKoreanJAVPreferredTerms(source, value string) string {
	switch strings.TrimSpace(source) {
	case "なっち":
		return "낫치"
	case "みぃたん":
		return "미이짱"
	case "百合川さら":
		return "유리카와 사라"
	case "久留木玲":
		return "쿠루키 레이"
	case "三尾めぐ":
		return "미오 메구"
	case "桜美ゆきな":
		return "사쿠라미 유키나"
	case "ピュアで物静かなボブJ●":
		return "Unknown"
	}
	if strings.Contains(source, "レロレロ") {
		value = strings.NewReplacer(
			"페로페로", "레로레로",
			"레로 레로", "레로레로",
			"레로레코", "레로레로",
		).Replace(value)
	}
	if strings.Contains(source, "メン地下") {
		value = strings.NewReplacer(
			"지하 남돌", "지하남돌",
			"지하 남성 아이돌", "지하남돌",
		).Replace(value)
	}
	if strings.Contains(source, "交縁界隈") {
		value = strings.NewReplacer(
			"교연계", "길거리 조건만남 판",
			"교엔 계통", "길거리 조건만남 판",
			"코이엔 계통", "길거리 조건만남 판",
			"길거리 조건만남 판라는 걸", "길거리 조건만남 판을",
		).Replace(value)
	}
	if strings.Contains(source, "立ちんぼ") {
		value = strings.ReplaceAll(value, "길빵", "길거리 성매매")
	}
	if strings.Contains(source, "猫じゃらし") {
		value = strings.NewReplacer(
			"고양이 낚시놀이", "고양이 장난감",
			"고양이 낚싯대", "고양이 장난감",
		).Replace(value)
	}
	if strings.Contains(source, "床上手") {
		value = strings.NewReplacer(
			"잠자리 고수", "섹스 고수",
			"상위호환", "섹스 고수",
		).Replace(value)
	}
	if strings.Contains(source, "精子") {
		value = strings.ReplaceAll(value, "정량", "정액")
	}
	if strings.Contains(source, "十代現役J") {
		value = strings.ReplaceAll(value, "1인칭 현역 J", "10대 현역 J")
	}
	if strings.Contains(source, "相場は") && strings.Contains(source, ".5") {
		value = strings.NewReplacer(
			"시세는 1.5엔부터", "시세는 1만 5천 엔부터",
			"시세는 1.5부터", "시세는 1만 5천 엔부터",
			"시세는 1.5~", "시세는 1만 5천 엔부터",
			"시세는 1.5～", "시세는 1만 5천 엔부터",
		).Replace(value)
	}
	if strings.Contains(source, "ホ別") {
		value = strings.ReplaceAll(value, "호텔비 별도 2엔", "호텔비 별도 2만 엔")
	}
	if strings.Contains(source, "パコ撮り") {
		value = strings.NewReplacer(
			"파코촬", "섹스 촬영",
			"파코 촬영", "섹스 촬영",
			"파코촬영", "섹스 촬영",
		).Replace(value)
	}
	if strings.Contains(source, "ハメ撮り") {
		value = strings.ReplaceAll(value, "셀프카메라", "셀프 섹스 촬영")
	}
	if strings.Contains(source, "ヤリモク") {
		value = strings.NewReplacer(
			"야리모쿠", "섹스만 노리는",
			"야리모크", "섹스만 노리는",
		).Replace(value)
	}
	if strings.Contains(source, "言いなり") {
		value = strings.ReplaceAll(value, "말이라면 뭐든 따르는 복종하는", "말이라면 뭐든 따르는")
	}
	if strings.Contains(source, "隠れた") {
		value = strings.ReplaceAll(value, "숨겨된", "숨은")
	}
	if strings.Contains(source, "絶品") {
		value = strings.ReplaceAll(value, "절품", "최고의")
	}
	if strings.Contains(source, "ハメ撮り映像流出") {
		value = strings.ReplaceAll(value, "셀프 섹스 촬영 영상 유무", "셀프 섹스 촬영 영상 유출")
	}
	if strings.Contains(source, "嫌われた底辺カメコ") {
		value = strings.ReplaceAll(value, "미움받는 밑바닥 카메코", "기피당하는 밑바닥 코스프레 촬영자")
	}
	if strings.Contains(source, "18歳のパイパンボディ") {
		value = strings.ReplaceAll(value, "18세의 백보지 몸매", "18세의 백보지")
	}
	if strings.Contains(source, "素股") {
		value = strings.ReplaceAll(value, "스마타", "가랑이딸")
	}
	if strings.Contains(source, "イラマ") {
		value = strings.NewReplacer(
			"딥스로트", "이라마치오",
			"이라마로", "이라마치오로",
			"이라마를", "이라마치오를",
			"이라마가", "이라마치오가",
			"이라마·", "이라마치오·",
			"이라마 ", "이라마치오 ",
		).Replace(value)
	}
	if strings.Contains(source, "ズボズボ") {
		value = strings.NewReplacer(
			"자지 즈보즈보", "자지로 깊숙이 쑤셔박기",
			"즈보즈보", "깊숙이 쑤셔박기",
		).Replace(value)
	}
	if strings.Contains(source, "生チン") || strings.Contains(source, "生ちん") || strings.Contains(source, "生チ○ポ") {
		value = strings.ReplaceAll(value, "생자지", "자지")
	}
	if strings.Contains(source, "激クンニ") {
		value = strings.ReplaceAll(value, "격렬한 쿤니", "격렬한 보빨")
	}
	if strings.Contains(source, "デカチン") {
		value = strings.ReplaceAll(value, "대물 자지", "대물")
	}
	if strings.Contains(source, "2穴") {
		value = strings.ReplaceAll(value, "2두 구멍", "2홀")
	}
	if strings.Contains(source, "痴●師") {
		value = strings.ReplaceAll(value, "치녀", "치한")
	}
	if strings.Contains(source, "第21弾") {
		value = strings.ReplaceAll(value, "제2릿탄", "제21탄")
	}
	if strings.Contains(source, "1年ぶり") {
		value = strings.NewReplacer(
			"1 오랜만의", "1년 만의",
			"1 오랜만", "1년 만",
		).Replace(value)
	}
	if strings.Contains(source, "そこもっとしてして") {
		value = strings.ReplaceAll(value, "더 해정해줘", "더 해줘")
	}
	if strings.Contains(source, "無理無理") {
		value = strings.ReplaceAll(value, "무리 무인", "무리야, 무리야")
	}
	if strings.Contains(source, "ランジェリー") {
		value = strings.ReplaceAll(value, "란지리", "란제리")
	}
	if strings.Contains(source, "際立たせる") {
		value = strings.ReplaceAll(value, "돋라게", "돋보이게")
	}
	if strings.Contains(source, "猛ピス") || strings.Contains(source, "ピストン") {
		value = strings.ReplaceAll(value, "피스턴", "피스톤")
	}
	if strings.Contains(source, "ダーツナンパ") {
		value = strings.NewReplacer(
			"다츠 난파", "다트 헌팅",
			"다트 난파", "다트 헌팅",
			"다츠 헌팅", "다트 헌팅",
		).Replace(value)
	}
	if strings.Contains(source, "極妻") {
		value = strings.NewReplacer(
			"극처녀 아내", "야쿠자 아내",
			"극강의 아내", "야쿠자 아내",
		).Replace(value)
	}
	if strings.Contains(source, "発禁") {
		value = strings.ReplaceAll(value, "발금 ", "발매 금지 ")
		switch {
		case strings.HasPrefix(value, "금지 "):
			value = "발매 금지 " + strings.TrimPrefix(value, "금지 ")
		}
	}
	if strings.HasSuffix(strings.TrimSpace(source), "なお") && strings.HasSuffix(strings.TrimSpace(value), "게다가") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "게다가")) + " 나오"
	}
	return value
}

func stripVRMarkers(title string) string {
	cleaned := vrMarkerRE.ReplaceAllString(title, "")
	return strings.TrimSpace(asciiSpaceRunRE.ReplaceAllString(cleaned, " "))
}

func stripPromoMarkers(title string) string {
	cleaned := promoMarkerRE.ReplaceAllString(title, "")
	return strings.TrimSpace(asciiSpaceRunRE.ReplaceAllString(cleaned, " "))
}

func isLikelyRomanized(value string) bool {
	for _, r := range value {
		if r > 0x024f {
			return false
		}
	}
	return true
}

func containsTranslatableText(value string) bool {
	for _, r := range value {
		if isResidualJapaneseRune(r) || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

func containsHangul(value string) bool {
	for _, r := range value {
		if r >= 0xac00 && r <= 0xd7a3 {
			return true
		}
	}
	return false
}

func isResidualJapaneseRune(r rune) bool {
	// Katakana punctuation is valid in otherwise Korean metadata. The middle
	// dot separates names and title fragments, while the prolonged sound mark
	// is often preserved as a dash-like title separator. Neither is evidence
	// of an untranslated Japanese word by itself.
	if r == '・' || r == 'ー' {
		return false
	}
	return r >= 0x3040 && r <= 0x30ff || r >= 0x3400 && r <= 0x4dbf || r >= 0x4e00 && r <= 0x9fff
}

func containsResidualJapanese(value string) bool { return countResidualJapanese(value) > 0 }

func residualJapaneseExcerpt(value string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 12
	}
	excerpt := make([]rune, 0, maxRunes)
	started := false
	for _, r := range value {
		if isResidualJapaneseRune(r) {
			started = true
			excerpt = append(excerpt, r)
			if len(excerpt) >= maxRunes {
				break
			}
			continue
		}
		if started {
			break
		}
	}
	return string(excerpt)
}

func countResidualJapanese(value string) int {
	count := 0
	for _, r := range value {
		if isResidualJapaneseRune(r) {
			count++
		}
	}
	return count
}

func restoreNamePlaceholders(text, source string, placeholders map[string]string) (string, bool) {
	for token, hangul := range placeholders {
		if strings.Count(text, token) != strings.Count(source, token) {
			return text, false
		}
		text = replaceNameToken(text, token, hangul)
	}
	if strings.Contains(text, "⟦") || strings.Contains(text, "⟧") {
		return text, false
	}
	return text, true
}

func replaceNameToken(text, token, hangul string) string {
	hasBatchim, jong := lastSyllableBatchim(hangul)
	var result strings.Builder
	for {
		index := strings.Index(text, token)
		if index < 0 {
			result.WriteString(text)
			return result.String()
		}
		result.WriteString(text[:index])
		result.WriteString(hangul)
		text = correctLeadingParticle(text[index+len(token):], hasBatchim, jong)
	}
}

func lastSyllableBatchim(value string) (bool, int) {
	var last rune
	for _, r := range value {
		if r >= 0xac00 && r <= 0xd7a3 {
			last = r
		}
	}
	if last == 0 {
		return false, 0
	}
	jong := int((last - 0xac00) % 28)
	return jong != 0, jong
}

var koParticlePairs = map[rune][2]rune{
	'은': {'은', '는'}, '는': {'은', '는'}, '이': {'이', '가'}, '가': {'이', '가'},
	'을': {'을', '를'}, '를': {'을', '를'}, '과': {'과', '와'}, '와': {'과', '와'},
	'아': {'아', '야'}, '야': {'아', '야'},
}

func correctLeadingParticle(value string, hasBatchim bool, jong int) string {
	if strings.HasPrefix(value, "으로") || strings.HasPrefix(value, "로") {
		body := strings.TrimPrefix(value, "으로")
		if body == value {
			body = strings.TrimPrefix(value, "로")
		}
		if hasBatchim && jong != 8 {
			return "으로" + body
		}
		return "로" + body
	}
	first, _ := utf8.DecodeRuneInString(value)
	if pair, ok := koParticlePairs[first]; ok {
		wanted := pair[1]
		if hasBatchim {
			wanted = pair[0]
		}
		return string(wanted) + value[len(string(first)):]
	}
	return value
}

func isPersonNameField(field string) bool {
	return strings.HasPrefix(field, "actress[") || field == "title_as_name"
}

func extractNamesFromDMMActjpgsURL(url string) (lastName, firstName string, ok bool) {
	const prefix = "actjpgs/"
	index := strings.LastIndex(url, prefix)
	if index < 0 {
		return "", "", false
	}
	filename := url[index+len(prefix):]
	if query := strings.IndexByte(filename, '?'); query >= 0 {
		filename = filename[:query]
	}
	if dot := strings.LastIndexByte(filename, '.'); dot >= 0 {
		filename = filename[:dot]
	}
	filename = strings.TrimRight(filename, "0123456789_")
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) == 1 && parts[0] != "" {
		return "", nihonshikiToHepburn.Replace(parts[0]), true
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return nihonshikiToHepburn.Replace(parts[0]), nihonshikiToHepburn.Replace(parts[1]), true
}

// ApplyDMMHepburnName derives a Hepburn reading from a DMM actress image URL.
func ApplyDMMHepburnName(actress *models.Actress) bool {
	if actress == nil {
		return false
	}
	last, first, ok := extractNamesFromDMMActjpgsURL(actress.ThumbURL)
	if !ok {
		return false
	}
	changed := false
	if strings.TrimSpace(actress.FirstName) == "" || models.IsUnknownActressName(actress.FirstName) {
		actress.FirstName, changed = first, first != ""
	}
	if strings.TrimSpace(actress.LastName) == "" || models.IsUnknownActressName(actress.LastName) {
		actress.LastName = last
		changed = changed || last != ""
	}
	return changed
}

func romanizedActressName(actress models.Actress) string {
	first := strings.TrimSpace(actress.FirstName)
	last := strings.TrimSpace(actress.LastName)
	if first == "" || !isLikelyRomanized(first) || last != "" && !isLikelyRomanized(last) {
		return ""
	}
	return strings.TrimSpace(last + " " + first)
}

func joinRomanizedName(lastName, firstName string) string {
	capitalize := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		return strings.ToUpper(value[:1]) + value[1:]
	}
	lastName = capitalize(lastName)
	firstName = capitalize(firstName)
	return strings.TrimSpace(lastName + " " + firstName)
}

func hangulActressName(actress models.Actress) string {
	last := strings.TrimSpace(actress.LastName)
	first := strings.TrimSpace(actress.FirstName)
	switch {
	case containsHangul(last) && containsHangul(first):
		return last + " " + first
	case containsHangul(first):
		return first
	case containsHangul(last):
		return last
	default:
		return ""
	}
}
