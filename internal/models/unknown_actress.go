package models

import "strings"

const UnknownActressName = "Unknown"

var unknownActressNameAliases = map[string]struct{}{
	"unknown":         {},
	"unknown actress": {},
	"unknown actor":   {},
	"미지수":           {},
	"미상":            {},
	"알수없음":          {},
	"알수 없음":         {},
	"알 수 없음":        {},
	"알 수 없는":        {},
	"불명":            {},
}

func normalizeUnknownActressKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

// IsUnknownActressName reports whether name is a placeholder for missing actress data.
func IsUnknownActressName(name string) bool {
	key := normalizeUnknownActressKey(name)
	if key == "" {
		return false
	}
	if _, ok := unknownActressNameAliases[key]; ok {
		return true
	}
	compact := strings.ReplaceAll(key, " ", "")
	_, ok := unknownActressNameAliases[compact]
	return ok
}

// IsUnknownActressFields reports whether all provided actress name fields are
// just a missing-data placeholder.
func IsUnknownActressFields(lastName, firstName, japaneseName string) bool {
	if jaName := strings.TrimSpace(japaneseName); jaName != "" && !IsUnknownActressName(jaName) {
		return false
	}
	if IsUnknownActressName(strings.TrimSpace(lastName+" "+firstName)) ||
		IsUnknownActressName(strings.TrimSpace(firstName+" "+lastName)) {
		return true
	}

	hasPlaceholder := false
	for _, name := range []string{lastName, firstName, japaneseName} {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !IsUnknownActressName(name) {
			return false
		}
		hasPlaceholder = true
	}
	return hasPlaceholder
}

// descriptiveNonNameMarkers are structural substrings that appear in scraper
// "actress" values that are actually promotional description blurbs, not real names —
// e.g. "【あいちゃん/24歳/173cm！！Iカップの美女OL！！】…". Real actress names never contain
// these, so their presence reliably flags a non-name value.
var descriptiveNonNameMarkers = []string{
	"【", "】", "［", "］", // bracketed blurb segments
	"！！", // promotional double-exclamation
}

// descriptorKeywords are relation/occupation/attribute terms scrapers append to (or
// substitute for) amateur names — age, cup size, height, marital/occupation labels,
// etc. They are used both to strip decorated tokens from a name and to flag a value
// that is a description with no name at all ("欲求不満セレブ妻"). Kept to strong terms
// that essentially never occur inside a real personal name.
var descriptorKeywords = []string{
	"歳", "才", "カップ", "ｶｯﾌﾟ", "cm", "ｃｍ", // age / cup / height
	"妻", "人妻", "若妻", "熟女", "主婦", "奥さん", // marital / relation
	"セレブ", "嬢", "キャバ", "ラウンジ", "勤務", // occupation / venue
	"女子大生", "大学", "年生", "専門学生", "職業", // student / occupation
	"OL", "ＯＬ", "素人", // office lady / amateur
}

// ContainsDescriptorKeyword reports whether s contains any relation/occupation/
// attribute descriptor keyword.
func ContainsDescriptorKeyword(s string) bool {
	for _, kw := range descriptorKeywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// descriptiveNonNameMaxRunes is a length backstop: real actress names never approach
// this many runes, so anything longer is treated as a descriptive blurb even if it
// carries none of the explicit markers above.
const descriptiveNonNameMaxRunes = 20

// IsDescriptiveNonName reports whether the actress name fields hold a promotional
// description blurb rather than a real personal name. Such values must not be used
// as an actress name (they get transliterated verbatim, producing absurdly long
// output); callers canonicalize them to Unknown instead.
func IsDescriptiveNonName(lastName, firstName, japaneseName string) bool {
	for _, field := range []string{lastName, firstName, japaneseName} {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		for _, marker := range descriptiveNonNameMarkers {
			if strings.Contains(field, marker) {
				return true
			}
		}
		if ContainsDescriptorKeyword(field) {
			return true
		}
		if len([]rune(field)) > descriptiveNonNameMaxRunes {
			return true
		}
	}
	return false
}

// CanonicalizeUnknownActress normalizes placeholder actress names to literal "Unknown".
func CanonicalizeUnknownActress(actress *Actress) bool {
	if actress == nil {
		return false
	}
	if !IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
		return false
	}
	actress.FirstName = UnknownActressName
	actress.LastName = ""
	actress.JapaneseName = UnknownActressName
	return true
}

// CanonicalizeUnknownActressInfo normalizes scraper placeholder actress names to literal "Unknown".
func CanonicalizeUnknownActressInfo(actress *ActressInfo) bool {
	if actress == nil {
		return false
	}
	if !IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
		return false
	}
	actress.FirstName = UnknownActressName
	actress.LastName = ""
	actress.JapaneseName = UnknownActressName
	return true
}
