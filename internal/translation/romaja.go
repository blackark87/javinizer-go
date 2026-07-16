package translation

import "strings"

var longVowelReplacer = strings.NewReplacer(
	"ā", "a", "Ā", "A",
	"ū", "u", "Ū", "U",
	"ō", "o", "Ō", "O",
	"ē", "e", "Ē", "E",
	"ī", "i", "Ī", "I",
)

func normalizeRomanizationToASCII(s string) string {
	return longVowelReplacer.Replace(s)
}

// romajiSyllables maps Hepburn romaji syllables to precomposed Hangul without a
// final consonant (batchim). Longest-match parsing tries 3-, then 2-, then
// 1-character keys. Nihon-shiki digraph spellings (sya, tya, zya) are included
// as tolerance for romanizations that bypass nihonshikiToHepburn.
var romajiSyllables = map[string]rune{
	"a": '아', "i": '이', "u": '우', "e": '에', "o": '오',
	"ka": '카', "ki": '키', "ku": '쿠', "ke": '케', "ko": '코',
	"ga": '가', "gi": '기', "gu": '구', "ge": '게', "go": '고',
	"sa": '사', "si": '시', "su": '스', "se": '세', "so": '소',
	"sha": '샤', "shi": '시', "shu": '슈', "she": '셰', "sho": '쇼',
	"za": '자', "zi": '지', "zu": '즈', "ze": '제', "zo": '조',
	"ja": '쟈', "ji": '지', "ju": '쥬', "je": '제', "jo": '죠',
	"ta": '타', "te": '테', "to": '토',
	"chi": '치', "cha": '챠', "chu": '츄', "che": '체', "cho": '쵸',
	"tsu": '츠',
	"da":  '다', "de": '데', "do": '도',
	"na": '나', "ni": '니', "nu": '누', "ne": '네', "no": '노',
	"ha": '하', "hi": '히', "hu": '후', "fu": '후', "he": '헤', "ho": '호',
	"ba": '바', "bi": '비', "bu": '부', "be": '베', "bo": '보',
	"pa": '파', "pi": '피', "pu": '푸', "pe": '페', "po": '포',
	"ma": '마', "mi": '미', "mu": '무', "me": '메', "mo": '모',
	"ya": '야', "yu": '유', "yo": '요',
	"ra": '라', "ri": '리', "ru": '루', "re": '레', "ro": '로',
	"wa": '와', "wo": '오',
	"kya": '캬', "kyu": '큐', "kyo": '쿄',
	"gya": '갸', "gyu": '규', "gyo": '교',
	"nya": '냐', "nyu": '뉴', "nyo": '뇨',
	"hya": '햐', "hyu": '휴', "hyo": '효',
	"bya": '뱌', "byu": '뷰', "byo": '뵤',
	"pya": '퍄', "pyu": '퓨', "pyo": '표',
	"mya": '먀', "myu": '뮤', "myo": '묘',
	"rya": '랴', "ryu": '류', "ryo": '료',
	"sya": '샤', "syu": '슈', "syo": '쇼',
	"tya": '챠', "tyu": '츄', "tyo": '쵸',
	"zya": '쟈', "zyu": '쥬', "zyo": '죠',
}

const (
	hangulBase     = rune(0xAC00)
	hangulEnd      = rune(0xD7A3)
	jongseongNieun = 4  // ㄴ final consonant index
	jongseongSiot  = 19 // ㅅ final consonant index
)

// addBatchim attaches a final consonant to a batchim-less Hangul syllable.
func addBatchim(r rune, jongseong rune) (rune, bool) {
	if r < hangulBase || r > hangulEnd || (r-hangulBase)%28 != 0 {
		return r, false
	}
	return r + jongseong, true
}

func isRomajiVowel(b byte) bool {
	return b == 'a' || b == 'i' || b == 'u' || b == 'e' || b == 'o'
}

// romajiWordToHangul transliterates a single lowercase romaji word.
func romajiWordToHangul(word string) (string, bool) {
	out := make([]rune, 0, len(word))

	attach := func(jongseong rune) bool {
		if len(out) == 0 {
			return false
		}
		r, ok := addBatchim(out[len(out)-1], jongseong)
		if !ok {
			return false
		}
		out[len(out)-1] = r
		return true
	}

	// prevVowel is the vowel the last emitted syllable ended on, or 0 when the
	// previous token did not end in a plain vowel (start of word, after ん/sokuon).
	// It drives long-vowel collapsing.
	var prevVowel byte

	i := 0
	for i < len(word) {
		c := word[i]

		// Apostrophe marks a syllable boundary after ん (Jun'ichi) — skip it.
		if c == '\'' {
			i++
			prevVowel = 0
			continue
		}

		// Sokuon (っ): a doubled consonant (kk/ss/tt/pp/cc) or the t of "tch"
		// becomes ㅅ batchim on the previous syllable (Mikka → 밋카).
		if i+1 < len(word) && (c == 'k' || c == 's' || c == 't' || c == 'p' || c == 'c') &&
			(word[i+1] == c || (c == 't' && strings.HasPrefix(word[i:], "tch"))) {
			if !attach(jongseongSiot) {
				return "", false
			}
			i++
			prevVowel = 0
			continue
		}

		// ん: n (or m before b/m/p, as in Homma) not followed by a vowel or y
		// becomes ㄴ batchim on the previous syllable (Ren → 렌, Kanna → 칸나).
		if c == 'n' && (i+1 == len(word) || (!isRomajiVowel(word[i+1]) && word[i+1] != 'y')) {
			if !attach(jongseongNieun) {
				return "", false
			}
			i++
			prevVowel = 0
			continue
		}
		if c == 'm' && i+1 < len(word) && (word[i+1] == 'b' || word[i+1] == 'm' || word[i+1] == 'p') {
			if !attach(jongseongNieun) {
				return "", false
			}
			i++
			prevVowel = 0
			continue
		}

		// Long vowel: a bare vowel that merely lengthens the previous syllable is
		// dropped, per the Korean convention of not writing Japanese長音
		// (Reena → 레나, Yuu → 유, Tarou → 타로). A doubled vowel (aa/ii/uu/ee/oo)
		// or "ou" lengthens; distinct morae (Yui, Aoi, Mai) are kept.
		if isRomajiVowel(c) && (c == prevVowel || (c == 'u' && prevVowel == 'o')) {
			i++
			continue
		}

		// Longest-match syllable lookup (3 → 2 → 1 characters).
		matched := false
		for l := 3; l >= 1; l-- {
			if i+l > len(word) {
				continue
			}
			if r, ok := romajiSyllables[word[i:i+l]]; ok {
				out = append(out, r)
				prevVowel = word[i+l-1] // every syllable key ends in its vowel
				i += l
				matched = true
				break
			}
		}
		if !matched {
			return "", false
		}
	}

	if len(out) == 0 {
		return "", false
	}
	return string(out), true
}

// romajiToHangul deterministically transliterates a Hepburn-romanized Japanese
// name into Hangul, one syllable at a time ("Hibiki Ren" → "히비키 렌",
// "Futaba Reena" → "후타바 레나"). Japanese long vowels are not written in Korean,
// so lengthening vowels are dropped (Yuu → 유, Tarou → 타로). Returns false when
// any word cannot be parsed as Japanese romaji, so callers can fall back to the LLM.
func romajiToHangul(name string) (string, bool) {
	words := strings.Fields(normalizeRomanizationToASCII(strings.ToLower(strings.TrimSpace(name))))
	if len(words) == 0 {
		return "", false
	}

	converted := make([]string, 0, len(words))
	for _, word := range words {
		hangul, ok := romajiWordToHangul(word)
		if !ok {
			return "", false
		}
		converted = append(converted, hangul)
	}
	return strings.Join(converted, " "), true
}
