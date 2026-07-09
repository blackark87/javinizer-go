package translation

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
)

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
