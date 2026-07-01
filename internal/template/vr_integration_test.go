package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/stretchr/testify/assert"
)

func TestVRConditionalTemplate(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		template string
		width    int
		height   int
		want     string
	}{
		{
			name:     "no mediainfo - not VR",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    0,
			height:   0,
			want:     "STSK-074.mp4",
		},
		{
			name:     "flat 16:9 1080p - not VR",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    1920,
			height:   1080,
			want:     "STSK-074.mp4",
		},
		{
			name:     "flat 16:9 4K - not VR",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    3840,
			height:   2160,
			want:     "STSK-074.mp4",
		},
		{
			name:     "VR180 SBS 4K (3840x1920)",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    3840,
			height:   1920,
			want:     "STSK-074-VR.mp4",
		},
		{
			name:     "VR180 SBS 8K (7680x3840)",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    7680,
			height:   3840,
			want:     "STSK-074-VR.mp4",
		},
		{
			name:     "top-bottom VR (1920x3840)",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    1920,
			height:   3840,
			want:     "STSK-074-VR.mp4",
		},
		{
			name:     "low-res coincidental 2:1 - below resolution floor",
			template: "<ID><IF:VR>-VR</IF>.mp4",
			width:    1280,
			height:   640,
			want:     "STSK-074.mp4",
		},
		{
			name:     "IF/ELSE branches on VR",
			template: "<ID><IF:VR>-VR<ELSE>-2D</IF>.mp4",
			width:    3840,
			height:   1920,
			want:     "STSK-074-VR.mp4",
		},
		{
			name:     "IF/ELSE falls back for non-VR",
			template: "<ID><IF:VR>-VR<ELSE>-2D</IF>.mp4",
			width:    1920,
			height:   1080,
			want:     "STSK-074-2D.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{ID: "STSK-074"}
			if tt.width > 0 || tt.height > 0 {
				ctx.cachedMediaInfo = &mediainfo.VideoInfo{Width: tt.width, Height: tt.height}
			}
			got, err := engine.Execute(tt.template, ctx)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
