package downloader

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMediaHandledForConsume(t *testing.T) {
	tests := []struct {
		name    string
		results []DownloadResult
		want    bool
	}{
		{name: "no selected media", want: true},
		{
			name: "all selected media handled",
			results: []DownloadResult{
				{Type: MediaTypeCover, LocalPath: "/media/fanart.jpg"},
				{Type: MediaTypePoster, LocalPath: "/media/poster.jpg"},
			},
			want: true,
		},
		{
			name: "optional screenshot 404 may be skipped",
			results: []DownloadResult{
				{Type: MediaTypeCover, LocalPath: "/media/fanart.jpg"},
				{Type: MediaTypeExtrafanart, Error: &statusError{statusCode: http.StatusNotFound}},
			},
			want: true,
		},
		{
			name: "optional trailer 404 may be wrapped",
			results: []DownloadResult{
				{Type: MediaTypeTrailer, Error: fmt.Errorf("download failed: %w", &statusError{statusCode: http.StatusNotFound})},
			},
			want: true,
		},
		{
			name: "poster 404 blocks destructive cleanup",
			results: []DownloadResult{
				{Type: MediaTypePoster, Error: &statusError{statusCode: http.StatusNotFound}},
			},
		},
		{
			name: "optional media 503 blocks destructive cleanup",
			results: []DownloadResult{
				{Type: MediaTypeExtrafanart, Error: &statusError{statusCode: http.StatusServiceUnavailable}},
			},
		},
		{
			name: "actress image is outside metadata media lifecycle",
			results: []DownloadResult{
				{Type: MediaTypeActress, Error: fmt.Errorf("profile image unavailable")},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mediaHandledForConsume(tt.results))
		})
	}
}
