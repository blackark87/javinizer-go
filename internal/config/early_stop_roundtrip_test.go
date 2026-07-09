package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestScrapersConfig_EarlyStopRoundTrip(t *testing.T) {
	src := ScrapersConfig{
		Priority:            []string{"dmm", "r18dev"},
		ScrapeActress:       true,
		EarlyStop:           true,
		EarlyStopMinResults: 3,
	}

	t.Run("json", func(t *testing.T) {
		data, err := json.Marshal(&src)
		if err != nil {
			t.Fatal(err)
		}
		var got ScrapersConfig
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !got.EarlyStop || got.EarlyStopMinResults != 3 {
			t.Fatalf("early_stop not preserved: %+v", got)
		}
		if _, bad := got.Overrides["early_stop"]; bad {
			t.Fatal("early_stop leaked into Overrides")
		}
	})

	t.Run("yaml", func(t *testing.T) {
		data, err := yaml.Marshal(&src)
		if err != nil {
			t.Fatal(err)
		}
		var got ScrapersConfig
		if err := yaml.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !got.EarlyStop || got.EarlyStopMinResults != 3 {
			t.Fatalf("early_stop not preserved (yaml): %+v", got)
		}
		if _, bad := got.Overrides["early_stop"]; bad {
			t.Fatal("early_stop leaked into Overrides (yaml)")
		}
	})
}
