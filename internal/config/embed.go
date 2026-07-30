package config

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

//go:embed config.yaml.example
var embeddedConfig []byte

func koreanJAVDictionaryFromEmbedded(fallback string) string {
	var document struct {
		Metadata struct {
			Translation struct {
				Dictionary string `yaml:"dictionary"`
			} `yaml:"translation"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(embeddedConfig, &document); err != nil {
		return fallback
	}
	if document.Metadata.Translation.Dictionary == "" {
		return fallback
	}
	return document.Metadata.Translation.Dictionary
}

// embeddedConfigBytes returns the raw embedded config bytes.
// Use this when you need the byte slice directly (e.g., for YAML parsing).
func embeddedConfigBytes() []byte {
	// Return a copy to prevent mutation of the embedded data
	result := make([]byte, len(embeddedConfig))
	copy(result, embeddedConfig)
	return result
}
