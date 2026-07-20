package translation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRomajiToHangul(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"final n", "Hibiki Ren", "히비키 렌", true},
		{"basic syllables", "Miyashita Rena", "미야시타 레나", true},
		{"long vowel ee dropped", "Futaba Reena", "후타바 레나", true},
		{"long vowel uu dropped", "Yuu", "유", true},
		{"long vowel ou dropped", "Tarou", "타로", true},
		{"confirmed ou mora boundary override", "Nonoura Non", "노노우라 논", true},
		{"long vowel ou in name", "Youko", "요코", true},
		{"long vowel oo dropped", "Oono", "오노", true},
		{"long vowel yuu dropped", "Yuuki", "유키", true},
		{"diphthong ei kept", "Reina", "레이나", true},
		{"distinct vowels ui kept", "Yui", "유이", true},
		{"distinct vowels aoi kept", "Aoi", "아오이", true},
		{"distinct vowels ai kept", "Mai", "마이", true},
		{"shi digraph", "Shinoda", "시노다", true},
		{"yoon sho", "Sho", "쇼", true},
		{"yoon kyo", "Kyoko", "쿄코", true},
		{"yoon ryu", "Ryu", "류", true},
		{"n before consonant", "Kanna", "칸나", true},
		{"n before b as m", "Homma", "혼마", true},
		{"sokuon kk", "Mikka", "밋카", true},
		{"sokuon tch", "Matcha", "맛챠", true},
		{"apostrophe n boundary", "Jun'ichi", "쥰이치", true},
		{"tsu", "Natsu", "나츠", true},
		{"fu", "Fujiko", "후지코", true},
		{"macron folding", "Yūna", "유나", true},
		{"nihon-shiki tolerance", "Syoko", "쇼코", true},
		{"single name", "Rima", "리마", true},
		{"non-japanese name", "Alice", "", false},
		{"western consonant cluster", "Christina", "", false},
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := romajiToHangul(tt.input)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
