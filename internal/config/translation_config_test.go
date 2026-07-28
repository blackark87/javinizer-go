package config

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestDefaultKoreanJAVDictionaryUsesDirectContextualTerminology(t *testing.T) {
	for _, expected := range []string{
		"성기·성행위·사정 표현은 원문의 노골성을 유지",
		"소중이, 그곳, 중요 부위 같은 완곡어로 순화하지 않음",
		"中出し -> 질내사정(제목·장르) / 안에 싸다(거친 대사·서술)",
		"ナックル手コキ -> 손가락 대딸",
		"フェラ / フェラチオ -> 펠라(제목·장르) / 자지 빨기(거친 행위 묘사)",
		"クンニ -> 보빨",
		"ちっぱい -> 빈유(컵 크기·신체 라벨) / 작은 가슴(자연스러운 서술)",
		"美巨乳 -> 예쁜 거유",
		"ちゃん / たん -> 이름 뒤의 호칭 접미사일 때 짱",
		"にゃんこちゃん -> 야옹이짱 또는 고양이짱",
		"爆美女 -> 초미녀",
		"桃尻 / 桃Siri -> 애플힙",
		"白桃尻 -> 뽀얀 애플힙",
		"ツルツルパイパンの綺麗なマ●コ -> 매끈하고 예쁜 백보지",
		"手マン -> 핑거링(제목·장르) / 보지를 손가락으로 쑤시다(거친 서술)",
		"ガシガシ手マン -> 거친 핑거링 / 보지를 손가락으로 거칠게 쑤시다",
		"潮吹き -> 분수 / 분수 폭발",
		"潮を部屋中に大噴出 -> 방 안 가득 분수 폭발",
		"潮吹き処女も頂いちゃった模様 -> 생애 첫 분수까지 터뜨려버린 듯",
		"ごっくん -> 정액 삼키기 / 정액을 삼키다",
		"即尺 -> 바로 빨기",
		"激ピス -> 격렬한 피스톤",
		"猛ピス -> 거친 피스톤 / 피스톤 맹공",
		"電クリ -> 전동 클리 자극",
		"電マ -> 전동 마사지기",
		"鬼イカセ -> 무자비 강제 절정",
		"ガン突き -> 쑤셔박기",
		"激イキ -> 격렬 절정",
		"ガン突き激イキ大放出セックス -> 쑤셔박기·격렬 절정·분수 대방출 섹스",
		"性獣 -> 색마",
		"ヤリモク -> 섹스만 노리는",
		"ザーメン / 精液 / 精子 -> 정액",
		"膣in / 膣イン -> 질 삽입",
		"生中 -> 노콘 질내사정",
		"現役J● -> 현역 J●",
		"円光 -> 조건만남",
		"床上手 -> 섹스 고수",
		"レロレロ -> 레로레로",
		"猫じゃらし -> 고양이 장난감",
		"交縁界隈 -> 길거리 조건만남 판",
		"立ちんぼ -> 길거리 성매매녀 / 길거리 성매매",
		"ホ別 + 숫자 -> 호텔비 별도 + 숫자만 엔",
		"相場は1.5～ -> 시세는 1만 5천 엔부터",
		"メン地下 -> 지하남돌",
		"ガーシー -> 가십",
		"西麻布 -> 니시아자부",
		"デカ乳＆デカ尻のムワッ感 -> 거유와 큰 엉덩이의 짙은 색기",
		"ハメ撮り -> POV 섹스 / 셀프 섹스 촬영",
		"シュートを決める -> 성적 JAV 문맥: 한 발 쏘다",
		"華麗なシュートを決めてきました -> 성적 JAV 문맥: 제대로 한 발 쏘고 왔습니다",
		"チームを勝利に導くマネージャーに華麗なシュートを決めてきました -> 팀을 승리로 이끄는 매니저에게 제대로 한 발 쏘고 왔습니다",
		"小動物系 / 小動物系美少女 -> 소동물계 / 소동물계 미소녀",
	} {
		assert.Contains(t, defaultKoreanJAVDictionary, expected)
	}
	for _, softened := range []string{
		"シュート -> 슛 (스포츠 문맥)",
		"ハメ撮り -> POV 또는 셀프카메라",
		"潮吹き -> 분수 또는 애액 분출",
		"潮吹き -> 분수 / 애액을 뿜다",
	} {
		assert.False(t, strings.Contains(defaultKoreanJAVDictionary, softened))
	}
}

func TestTranslationConfig_SettingsHash(t *testing.T) {
	t.Run("deterministic hash for same config", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title:       true,
				Description: true,
			},
			OpenAI: OpenAITranslationConfig{
				Model: "gpt-4",
			},
		}

		cfg2 := TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title:       true,
				Description: true,
			},
			OpenAI: OpenAITranslationConfig{
				Model: "gpt-4",
			},
		}

		hash1 := cfg1.SettingsHash()
		hash2 := cfg2.SettingsHash()

		assert.Equal(t, hash1, hash2, "same config should produce same hash")
		assert.Len(t, hash1, 16, "hash should be 16 characters")
	})

	t.Run("different hash for different provider", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{Model: "gpt-4"},
		}

		cfg2 := TranslationConfig{
			Provider:       "deepl",
			SourceLanguage: "ja",
			TargetLanguage: "en",
			DeepL:          DeepLTranslationConfig{Mode: "pro"},
		}

		hash1 := cfg1.SettingsHash()
		hash2 := cfg2.SettingsHash()

		assert.NotEqual(t, hash1, hash2, "different provider should produce different hash")
	})

	t.Run("different hash for different model", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{Model: "gpt-3.5-turbo"},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{Model: "gpt-4"},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different model should produce different hash")
	})

	t.Run("different hash for different target language", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "zh",
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different target language should produce different hash")
	})

	t.Run("same hash for different api_key", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{APIKey: "key1"},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			OpenAI:         OpenAITranslationConfig{APIKey: "key2"},
		}

		assert.Equal(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "api_key change should not affect hash")
	})

	t.Run("same hash for different timeout", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			TimeoutSeconds: 30,
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			TimeoutSeconds: 60,
		}

		assert.Equal(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "timeout change should not affect hash")
	})

	t.Run("different hash for different fields", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title: true,
			},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			Fields: TranslationFieldsConfig{
				Title:       true,
				Description: true,
			},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different fields should produce different hash")
	})

	t.Run("different hash for apply_to_primary change", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			ApplyToPrimary: false,
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			ApplyToPrimary: true,
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "apply_to_primary change should produce different hash")
	})

	t.Run("different hash for different google mode", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			Google:         GoogleTranslationConfig{Mode: "free"},
		}

		cfg2 := TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			Google:         GoogleTranslationConfig{Mode: "paid"},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "different google mode should produce different hash")
	})

	t.Run("same hash for language case variations", func(t *testing.T) {
		cfg1 := TranslationConfig{
			Provider:       "openai",
			SourceLanguage: "JA",
			TargetLanguage: "EN",
		}

		cfg2 := TranslationConfig{
			Provider:       "openai",
			SourceLanguage: "ja",
			TargetLanguage: "en",
		}

		assert.Equal(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "language case should not affect hash")
	})

	t.Run("different hash for openai-compatible thinking toggle", func(t *testing.T) {
		thinkingDisabled := false
		thinkingEnabled := true

		cfg1 := TranslationConfig{
			Provider:       "openai-compatible",
			TargetLanguage: "en",
			OpenAICompatible: OpenAICompatibleTranslationConfig{
				Model:          "qwen3",
				EnableThinking: &thinkingDisabled,
			},
		}

		cfg2 := TranslationConfig{
			Provider:       "openai-compatible",
			TargetLanguage: "en",
			OpenAICompatible: OpenAICompatibleTranslationConfig{
				Model:          "qwen3",
				EnableThinking: &thinkingEnabled,
			},
		}

		assert.NotEqual(t, cfg1.SettingsHash(), cfg2.SettingsHash(), "thinking toggle should affect hash")
	})

	t.Run("dictionary mode changes output hash", func(t *testing.T) {
		base := TranslationConfig{Provider: "openai", TargetLanguage: "ko", Dictionary: "中出し -> 질내사정"}
		dictionaryMode := base
		dictionaryMode.DictionaryEnabled = true

		assert.NotEqual(t, base.SettingsHash(), dictionaryMode.SettingsHash())
	})

	t.Run("enabled dictionary content changes output hash", func(t *testing.T) {
		first := TranslationConfig{
			Provider: "openai", TargetLanguage: "ko",
			DictionaryEnabled: true, Dictionary: "中出し -> 질내사정",
		}
		second := first
		second.Dictionary = "中出し -> 안에 싸기"

		assert.NotEqual(t, first.SettingsHash(), second.SettingsHash())
	})

	t.Run("inactive dictionary edits do not change output hash", func(t *testing.T) {
		first := TranslationConfig{Provider: "openai", TargetLanguage: "ko", Dictionary: "中出し -> 질내사정"}
		second := first
		second.Dictionary = "中出し -> 안에 싸기"

		assert.Equal(t, first.SettingsHash(), second.SettingsHash())
	})
}

func TestOpenAICompatibleTranslationConfig_NormalizedBackendType(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"auto", "auto", ""},
		{"auto with whitespace", " Auto ", ""},
		{"vllm", "vllm", "vllm"},
		{"vllm uppercase", "VLLM", "vllm"},
		{"ollama", "ollama", "ollama"},
		{"llama.cpp", "llama.cpp", "llama.cpp"},
		{"llamacpp", "llamacpp", "llama.cpp"},
		{"llama_cpp", "llama_cpp", "llama.cpp"},
		{"other", "other", "other"},
		{"generic", "generic", "other"},
		{"unknown passthrough", "mybackend", "mybackend"},
		{"unknown passthrough uppercase", "MyBackend", "mybackend"},
		{"whitespace trimmed", "  vllm  ", "vllm"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := OpenAICompatibleTranslationConfig{BackendType: tc.input}
			assert.Equal(t, tc.expected, cfg.NormalizedBackendType())
		})
	}
}

func TestOpenAICompatibleTranslationConfig_EffectiveEnableThinking(t *testing.T) {
	t.Run("nil returns false", func(t *testing.T) {
		cfg := OpenAICompatibleTranslationConfig{}
		assert.False(t, cfg.EffectiveEnableThinking())
	})

	t.Run("true pointer returns true", func(t *testing.T) {
		v := true
		cfg := OpenAICompatibleTranslationConfig{EnableThinking: &v}
		assert.True(t, cfg.EffectiveEnableThinking())
	})

	t.Run("false pointer returns false", func(t *testing.T) {
		v := false
		cfg := OpenAICompatibleTranslationConfig{EnableThinking: &v}
		assert.False(t, cfg.EffectiveEnableThinking())
	})
}

func TestOpenAICompatibleTranslationConfig_EffectiveMaxOutputTokens(t *testing.T) {
	assert.Equal(t, 4096, (OpenAICompatibleTranslationConfig{}).EffectiveMaxOutputTokens())
	assert.Equal(t, 2048, (OpenAICompatibleTranslationConfig{MaxOutputTokens: 2048}).EffectiveMaxOutputTokens())
}

func TestOpenAICompatibleTranslationConfig_NormalizedThinkingMode(t *testing.T) {
	for input, expected := range map[string]string{
		"": "boolean", "BOOLEAN": "boolean", "low": "low", "Medium": "medium", "HIGH": "high", "invalid": "boolean",
	} {
		t.Run(input, func(t *testing.T) {
			assert.Equal(t, expected, (OpenAICompatibleTranslationConfig{ThinkingMode: input}).NormalizedThinkingMode())
		})
	}
}

func TestTranslationConfig_SettingsHashIncludesThinkingMode(t *testing.T) {
	enabled := true
	base := TranslationConfig{Provider: "openai-compatible", OpenAICompatible: OpenAICompatibleTranslationConfig{Model: "model", EnableThinking: &enabled, ThinkingMode: "boolean"}}
	effort := base
	effort.OpenAICompatible.ThinkingMode = "high"
	assert.NotEqual(t, base.SettingsHash(), effort.SettingsHash())
}

func TestTranslationConfig_SettingsHashIncludesMaxOutputTokens(t *testing.T) {
	base := TranslationConfig{Provider: "openai-compatible", OpenAICompatible: OpenAICompatibleTranslationConfig{Model: "model", MaxOutputTokens: 4096}}
	limited := base
	limited.OpenAICompatible.MaxOutputTokens = 2048
	assert.NotEqual(t, base.SettingsHash(), limited.SettingsHash())
}

func TestNFOConfig_IsUnknownActressFallback(t *testing.T) {
	t.Run("returns true when mode is fallback", func(t *testing.T) {
		n := NFOConfig{Format: NFOFormatConfig{UnknownActressMode: models.UnknownActressModeFallback}}
		assert.True(t, n.IsUnknownActressFallback())
	})

	t.Run("returns false when mode is not fallback", func(t *testing.T) {
		n := NFOConfig{Format: NFOFormatConfig{UnknownActressMode: models.UnknownActressModeSkip}}
		assert.False(t, n.IsUnknownActressFallback())
	})

	t.Run("returns false when mode is empty", func(t *testing.T) {
		n := NFOConfig{}
		assert.False(t, n.IsUnknownActressFallback())
	})
}
