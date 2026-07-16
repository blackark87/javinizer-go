package translation

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"
)

var ageOccupationSuffixRE = regexp.MustCompile(`\s+[0-9０-９]+歳.*`)

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
	if strings.HasPrefix(name, "[") {
		if end := strings.LastIndex(name, "]"); end > 0 {
			name = strings.TrimSpace(name[1:end])
		}
	}
	if idx := strings.Index(name, ","); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	if idx := strings.Index(name, "・"); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	name = strings.TrimSpace(ageOccupationSuffixRE.ReplaceAllString(name, ""))
	honorifics := []string{"ちゃん", "くん", "さん", "様", "氏", "君"}
	if tokens := strings.Fields(name); len(tokens) > 1 {
		last := tokens[len(tokens)-1]
		for _, suffix := range honorifics {
			if strings.HasSuffix(last, suffix) {
				name = last
				break
			}
		}
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
	for _, suffix := range honorifics {
		if strings.HasSuffix(name, suffix) && len([]rune(name)) > len([]rune(suffix)) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	return strings.TrimSpace(name)
}

var descriptionPromoStoreRE = regexp.MustCompile(`(?:特集\s*)?最新作やセール商品など、お得な情報満載[の의]\s*『[^』]*KMPストア[^』]*』はこちら！?`)

var descriptionPromotionalAnchors = []string{
	"※この作品はバイノーラル録音されております", "※ この作品はバイノーラル録音されております",
	"※この商品は専用プレイヤーでの視聴に最適化されています", "※ この商品は専用プレイヤーでの視聴に最適化されています",
	"※VR専用作品は必ず下記リンクより動作環境・対応デバイス", "※ VR専用作品は必ず下記リンクより動作環境・対応デバイス",
	"「動作環境・対応デバイス」について", "※ 配信方法によって収録内容が異なる場合があります",
	"※配信方法によって収録内容が異なる場合があります", "特集 最新作やセール商品など、お得な情報満載",
	"最新作やセール商品など、お得な情報満載",
}

func cleanDescriptionForTranslation(description string) string {
	description = strings.TrimSpace(description)
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
var asciiSpaceRunRE = regexp.MustCompile(`[ \t]{2,}`)

func cleanTitleForTranslation(title string) string { return stripPromoMarkers(stripVRMarkers(title)) }

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
	return r >= 0x3040 && r <= 0x30ff || r >= 0x3400 && r <= 0x4dbf || r >= 0x4e00 && r <= 0x9fff
}

func containsResidualJapanese(value string) bool { return countResidualJapanese(value) > 0 }

func countResidualJapanese(value string) int {
	count := 0
	for _, r := range value {
		if isResidualJapaneseRune(r) {
			count++
		}
	}
	return count
}

func restoreNamePlaceholders(text string, placeholders map[string]string) (string, bool) {
	ok := true
	for token, hangul := range placeholders {
		if !strings.Contains(text, token) {
			ok = false
			continue
		}
		text = replaceNameToken(text, token, hangul)
	}
	return text, ok
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
