package translation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReplaceNameToken_ParticleAgreement(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		token  string
		hangul string
		want   string
	}{
		// vowel-final name (마유키): batchim-forms must flip to vowel-forms
		{"subject 이→가", "⟦0⟧이 등장", "⟦0⟧", "이토 마유키", "이토 마유키가 등장"},
		{"topic 은→는", "⟦0⟧은 최고", "⟦0⟧", "이토 마유키", "이토 마유키는 최고"},
		{"object 을→를", "⟦0⟧을 촬영", "⟦0⟧", "이토 마유키", "이토 마유키를 촬영"},
		{"and 과→와", "⟦0⟧과 친구", "⟦0⟧", "이토 마유키", "이토 마유키와 친구"},
		{"direction 으로→로", "⟦0⟧으로 향해", "⟦0⟧", "이토 마유키", "이토 마유키로 향해"},
		{"already correct 가 stays", "⟦0⟧가 등장", "⟦0⟧", "이토 마유키", "이토 마유키가 등장"},
		// consonant-final name (렌): vowel-forms must flip to batchim-forms
		{"subject 가→이", "⟦0⟧가 등장", "⟦0⟧", "히비키 렌", "히비키 렌이 등장"},
		{"topic 는→은", "⟦0⟧는 최고", "⟦0⟧", "히비키 렌", "히비키 렌은 최고"},
		{"object 를→을", "⟦0⟧를 촬영", "⟦0⟧", "히비키 렌", "히비키 렌을 촬영"},
		{"direction 로→으로", "⟦0⟧로 향해", "⟦0⟧", "히비키 렌", "히비키 렌으로 향해"},
		// ㄹ-batchim name (설): 으로 stays 로
		{"ril-batchim direction 로 stays", "⟦0⟧로 이동", "⟦0⟧", "마이세츠 카오루", "마이세츠 카오루로 이동"},
		// no particle following → unchanged
		{"space then word untouched", "⟦0⟧ 가방", "⟦0⟧", "이토 마유키", "이토 마유키 가방"},
		{"punctuation untouched", "⟦0⟧!", "⟦0⟧", "이토 마유키", "이토 마유키!"},
		// multiple occurrences
		{"two occurrences", "⟦0⟧이 ⟦0⟧을", "⟦0⟧", "이토 마유키", "이토 마유키가 이토 마유키를"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceNameToken(tt.text, tt.token, tt.hangul)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRestoreNamePlaceholders_Particles(t *testing.T) {
	placeholders := map[string]string{"⟦0⟧": "이토 마유키"}
	got, ok := restoreNamePlaceholders("⟦0⟧이 등장하는 작품", placeholders)
	assert.True(t, ok)
	assert.Equal(t, "이토 마유키가 등장하는 작품", got)

	// missing token → ok=false, text unchanged for that token
	_, ok2 := restoreNamePlaceholders("이름 없음", placeholders)
	assert.False(t, ok2)
}
