package mediainfo

import (
	"context"
	"fmt"
	"os"
)

const codecUnknown = "unknown"

// VideoInfo contains metadata extracted from a video file
type VideoInfo struct {
	// Video properties
	VideoCodec  string  // "h264", "hevc", "vp9", etc.
	Width       int     // Video width in pixels (e.g., 1920)
	Height      int     // Video height in pixels (e.g., 1080)
	Duration    float64 // Duration in seconds
	Bitrate     int     // Bitrate in kbps (computed from file size and duration)
	AspectRatio float64 // Aspect ratio (computed from width/height)
	FrameRate   float64 // Frames per second

	// Audio properties
	AudioCodec    string // "aac", "mp3", "ac3", etc.
	AudioChannels int    // Number of audio channels (2 = stereo, 6 = 5.1)
	SampleRate    int    // Audio sample rate (e.g., 48000, 44100)

	// Container
	Container string // "mp4", "mkv", "avi", etc.
}

// Analyze extracts metadata from a video file using the proberRegistry
// Supports: MP4, MKV, MOV, AVI
// Falls back to MediaInfo CLI if enabled and native parsers fail
// Returns partial info if some fields are unavailable
func Analyze(ctx context.Context, filePath string) (*VideoInfo, error) {
	return analyzeWithConfig(ctx, filePath, nil)
}

// analyzeWithConfig extracts metadata using custom configuration
func analyzeWithConfig(ctx context.Context, filePath string, cfg *mediaInfoConfig) (*VideoInfo, error) {
	// Honor cancellation before opening the media file so all callers get
	// consistent behavior (Analyze now accepts a context but previously opened
	// the file before checking it).
	if ctx == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("media analysis canceled before opening file: %w", err)
	}

	// Open file
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Initialize registry if needed
	if cfg == nil {
		cfg = defaultMediaInfoConfig()
	}
	registry := newProberRegistry(cfg)

	// Use registry to probe with fallback (f is *os.File which satisfies FileReader)
	info, err := registry.probeWithFallback(ctx, f)
	if err != nil {
		return nil, err
	}

	// Compute aspect ratio if dimensions available
	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	return info, nil
}

// detectContainer detects the container format from file header
func detectContainer(header []byte) string {
	// MP4/MOV: contains "ftyp" in first 12 bytes
	if len(header) >= 8 {
		// Check for ftyp box (byte 4-7)
		if header[4] == 'f' && header[5] == 't' && header[6] == 'y' && header[7] == 'p' {
			return "mp4"
		}
	}

	// MKV/WebM: EBML header starts with 0x1A 0x45 0xDF 0xA3
	if len(header) >= 4 {
		if header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3 {
			return "mkv"
		}
	}

	// AVI: starts with "RIFF" and contains "AVI " at offset 8
	if len(header) >= 12 {
		if header[0] == 'R' && header[1] == 'I' && header[2] == 'F' && header[3] == 'F' &&
			header[8] == 'A' && header[9] == 'V' && header[10] == 'I' {
			return "avi"
		}
	}

	return codecUnknown
}

// GetResolution returns human-readable resolution string.
// Examples: "8K", "4K", "1080p", "720p", "480p".
//
// Width is checked alongside Height at every tier (not just 8K) because
// side-by-side packed VR video (e.g. 180 SBS) doubles the width for the
// left/right eye but keeps a single-eye height, e.g. a "4K" VR180 SBS file
// is often 3840x1920 - height alone would misclassify it as 1080p.
func (v *VideoInfo) GetResolution() string {
	if v.Width >= 7680 || v.Height >= 4320 {
		return "8K"
	} else if v.Width >= 3840 || v.Height >= 2160 {
		return "4K"
	} else if v.Width >= 1920 || v.Height >= 1080 {
		return "1080p"
	} else if v.Width >= 1280 || v.Height >= 720 {
		return "720p"
	}
	return "480p"
}

const (
	// vrAspectMin/vrAspectMax bound the width/height ratio treated as VR framing.
	// 180 SBS and 360 mono-equirect pack two eyes/the full sphere side by side,
	// doubling width while keeping single-eye height (~2:1, e.g. 3840x1920,
	// 5760x2880, 7680x3840). Top-bottom VR mirrors this on height (~1:2).
	vrAspectMin = 1.9
	vrAspectMax = 2.1

	// vrMinDoubledDimension is a floor on the doubled dimension (Width for SBS,
	// Height for top-bottom). VR capture is always high resolution since each
	// eye only gets half the frame, so this avoids misclassifying a
	// low-resolution flat video that coincidentally has a ~2:1 aspect ratio.
	vrMinDoubledDimension = 2880
)

// IsVR reports whether the video's own dimensions indicate VR framing
// (180 SBS, top-bottom, or 360 mono-equirectangular), independent of any
// scraped metadata - the file itself is the source of truth.
func (v *VideoInfo) IsVR() bool {
	if v.Width <= 0 || v.Height <= 0 {
		return false
	}
	aspect := float64(v.Width) / float64(v.Height)
	if aspect >= vrAspectMin && aspect <= vrAspectMax {
		return v.Width >= vrMinDoubledDimension
	}
	if aspect >= 1/vrAspectMax && aspect <= 1/vrAspectMin {
		return v.Height >= vrMinDoubledDimension
	}
	return false
}

// getAudioChannelDescription returns human-readable audio channel description
// Examples: "Stereo", "5.1", "7.1"
//
//nolint:unused // used by same-package tests
func (v *VideoInfo) getAudioChannelDescription() string {
	switch v.AudioChannels {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		return fmt.Sprintf("%d channels", v.AudioChannels)
	}
}
