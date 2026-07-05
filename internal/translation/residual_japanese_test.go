package translation

import "testing"

func TestContainsResidualJapanese(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"clean korean", "그 격차가 최고다", false},
		{"korean with www", "최고www", false},
		{"korean with ascii digits", "360도 전방위", false},
		{"leftover hiragana", "그 격차가 최고すぎる", true},
		{"leftover katakana", "로데오 騎乗位ロデオ", true},
		{"leftover kanji", "그 격차가 最高", true},
		{"pure latin", "Fujiwara Erika", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsResidualJapanese(tt.in); got != tt.want {
				t.Errorf("containsResidualJapanese(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestCountResidualJapanese(t *testing.T) {
	if n := countResidualJapanese("최고すぎる"); n != 3 {
		t.Errorf("countResidualJapanese = %d, want 3", n)
	}
	if n := countResidualJapanese("완전한국어"); n != 0 {
		t.Errorf("countResidualJapanese = %d, want 0", n)
	}
}
