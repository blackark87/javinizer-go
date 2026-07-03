package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUnknownActressName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"english", "Unknown", true},
		{"english actor phrase", "unknown actor", true},
		{"korean unknown", "미상", true},
		{"korean no spaces", "알수없음", true},
		{"korean spaced", "알 수 없음", true},
		{"normal name", "Hatano Yui", false},
		{"empty", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsUnknownActressName(tc.in))
		})
	}
}

func TestCanonicalizeUnknownActress(t *testing.T) {
	actress := &Actress{FirstName: "미상", LastName: "ignored", JapaneseName: "미상"}

	assert.False(t, CanonicalizeUnknownActress(actress))
	assert.Equal(t, "미상", actress.FirstName)
	assert.Equal(t, "ignored", actress.LastName)
	assert.Equal(t, "미상", actress.JapaneseName)

	actress = &Actress{FirstName: "미상", JapaneseName: "알 수 없음"}

	assert.True(t, CanonicalizeUnknownActress(actress))
	assert.Equal(t, UnknownActressName, actress.FirstName)
	assert.Empty(t, actress.LastName)
	assert.Equal(t, UnknownActressName, actress.JapaneseName)
}
