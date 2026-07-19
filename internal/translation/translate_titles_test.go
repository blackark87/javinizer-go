package translation

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
)

func TestTranslateTitlesPreservesSourceBracketMeaning(t *testing.T) {
	var received []string
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		received = append([]string(nil), texts...)
		return &translationResult{Texts: []string{
			"[POV] 현립상업과. 주말 가출 POV 지원",
			"[POV] 오리지널 타이틀",
		}}, nil
	}}
	svc := New(Config{Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko"}, provider)

	out, err := svc.TranslateTitles(context.Background(), []string{
		"【個撮】県立商業科。週末の家出をハメ撮り支援",
		"[POV] オリジナルタイトル",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 || received[0] != "[개인촬영]県立商業科。週末の家出をハメ撮り支援" {
		t.Fatalf("expected protected private-shoot marker, got %v", received)
	}
	if out[0] != "[개인촬영] 현립상업과. 주말 가출 POV 지원" {
		t.Fatalf("unexpected private-shoot translation: %q", out[0])
	}
	if out[1] != "[POV] 오리지널 타이틀" {
		t.Fatalf("literal source POV marker should remain, got %q", out[1])
	}
}

func TestTranslateTitles_NoopPaths(t *testing.T) {
	in := []string{"タイトルA", "タイトルB"}

	t.Run("disabled returns input unchanged", func(t *testing.T) {
		svc := New(config.TranslationConfig{Enabled: false, TargetLanguage: "ko"})
		out, err := svc.TranslateTitles(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
			t.Fatalf("expected input unchanged, got %v", out)
		}
	})

	t.Run("no target language returns input", func(t *testing.T) {
		svc := New(config.TranslationConfig{Enabled: true})
		out, _ := svc.TranslateTitles(context.Background(), in)
		if out[0] != in[0] {
			t.Fatalf("expected input unchanged, got %v", out)
		}
	})

	t.Run("source equals target returns input", func(t *testing.T) {
		svc := New(config.TranslationConfig{Enabled: true, SourceLanguage: "ja", TargetLanguage: "ja"})
		out, _ := svc.TranslateTitles(context.Background(), in)
		if out[0] != in[0] {
			t.Fatalf("expected input unchanged, got %v", out)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		svc := New(config.TranslationConfig{Enabled: true, TargetLanguage: "ko"})
		out, _ := svc.TranslateTitles(context.Background(), nil)
		if len(out) != 0 {
			t.Fatalf("expected empty, got %v", out)
		}
	})
}
