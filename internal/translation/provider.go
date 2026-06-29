package translation

import "context"

// TranslatorProvider translates text between languages for a single translation backend.
type TranslatorProvider interface {
	Name() string
	Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error)
}
