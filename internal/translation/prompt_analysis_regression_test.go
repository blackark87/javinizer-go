package translation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKoreanJAVPromptReinforcesHighFrequencyProductionTerms(t *testing.T) {
	rules := koreanJAVPromptRules("ko")
	for _, expected := range []string{
		"極上→최고|최상급≠극상",
		"エロい→야한≠에로한",
		"compounds エロフラグ→에로 플래그,エロテク→에로 테크닉",
		"生ハメ→노콘",
		"潮吹き→분수|애액 분출|애액을 뿜다",
	} {
		assert.Contains(t, rules, expected)
	}

	_, userPrompt, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		[]string{"極上の生ハメ潮吹き エロい"},
		[]string{"<<<title>>>"},
	)
	require.NoError(t, err)
	for _, expected := range []string{
		"praise 極上→최고|최상급",
		"生ハメ→노콘",
		"sexual 潮吹き→분수|애액 분출",
		"ordinary adjective エロい→야한",
	} {
		assert.Contains(t, userPrompt, expected)
	}
}

func TestKoreanJAVPromptPreservesEroCompoundTerms(t *testing.T) {
	_, userPrompt, err := buildLLMTranslationPromptsWithMarkers(
		"ja",
		"ko",
		[]string{"エロフラグ エロテク"},
		[]string{"<<<title>>>"},
	)
	require.NoError(t, err)

	assert.Contains(t, userPrompt, "エロフラグ→에로 플래그")
	assert.Contains(t, userPrompt, "エロテク→에로 테크닉")
	assert.NotContains(t, userPrompt, "ordinary adjective エロい→야한")

	compactRules := koreanJAVPromptRules("ko", llmPromptOptions{
		dictionaryEnabled: true,
		dictionary:        "エロい -> 야한\nエロフラグ -> 에로 플래그",
	})
	assert.Contains(t, compactRules, "Prefer the longest matching dictionary entry")
	assert.Contains(t, compactRules, "never apply a shorter generic entry inside it")
}
