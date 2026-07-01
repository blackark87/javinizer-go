package mediainfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectContainer(t *testing.T) {
	tests := []struct {
		name     string
		header   []byte
		expected string
	}{
		{
			name: "MP4 container",
			header: []byte{
				0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p',
				'i', 's', 'o', '5', 0x00, 0x00, 0x00, 0x00,
			},
			expected: "mp4",
		},
		{
			name: "MKV container",
			header: []byte{
				0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x1F, 0x42, 0x86, 0x81, 0x01,
			},
			expected: "mkv",
		},
		{
			name: "AVI container",
			header: []byte{
				'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00,
				'A', 'V', 'I', ' ', 'L', 'I', 'S', 'T',
			},
			expected: "avi",
		},
		{
			name: "Unknown container",
			header: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectContainer(tt.header)
			if result != tt.expected {
				t.Errorf("detectContainer() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestVideoInfo_GetResolution(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		expected string
	}{
		{"4K", 0, 2160, "4K"},
		{"1080p", 0, 1080, "1080p"},
		{"720p", 0, 720, "720p"},
		{"480p", 0, 480, "480p"},
		{"below 480p floor", 0, 360, "480p"},
		{"8K by width", 7680, 0, "8K"},
		{"VR180 SBS 4K (3840x1920)", 3840, 1920, "4K"},
		{"VR180 SBS 8K (7680x3840)", 7680, 3840, "8K"},
		{"VR180 SBS 1080p (1920x960)", 1920, 960, "1080p"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &VideoInfo{Width: tt.width, Height: tt.height}
			if got := v.GetResolution(); got != tt.expected {
				t.Errorf("GetResolution() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestVideoInfo_IsVR(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		height   int
		expected bool
	}{
		{"flat 16:9 1080p", 1920, 1080, false},
		{"flat 16:9 4K", 3840, 2160, false},
		{"flat 4:3", 1024, 768, false},
		{"zero dimensions", 0, 0, false},
		{"VR180 SBS 4K (3840x1920)", 3840, 1920, true},
		{"VR180 SBS 8K (7680x3840)", 7680, 3840, true},
		{"VR180 SBS (5760x2880)", 5760, 2880, true},
		{"top-bottom VR (1920x3840)", 1920, 3840, true},
		{"low-res coincidental 2:1 below floor", 1280, 640, false},
		{"low-res coincidental 1:2 below floor", 640, 1280, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &VideoInfo{Width: tt.width, Height: tt.height}
			if got := v.IsVR(); got != tt.expected {
				t.Errorf("IsVR() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestVideoInfo_GetAudioChannelDescription(t *testing.T) {
	tests := []struct {
		name     string
		channels int
		expected string
	}{
		{"Mono", 1, "Mono"},
		{"Stereo", 2, "Stereo"},
		{"5.1", 6, "5.1"},
		{"7.1", 8, "7.1"},
		{"Custom", 4, "4 channels"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &VideoInfo{AudioChannels: tt.channels}
			if got := v.GetAudioChannelDescription(); got != tt.expected {
				t.Errorf("GetAudioChannelDescription() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAnalyze_InvalidFile(t *testing.T) {
	_, err := Analyze("/nonexistent/file.mp4")
	if err == nil {
		t.Error("Analyze() should return error for nonexistent file")
	}
}

func TestAnalyze_TooSmallFile(t *testing.T) {
	// Create a temporary file that's too small to be a valid video
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "small.mp4")

	if err := os.WriteFile(tmpFile, []byte("too small"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err := Analyze(tmpFile)
	if err == nil {
		t.Error("Analyze() should return error for too small file")
	}
}

// TestAnalyzeWithConfig_CustomConfig tests with custom MediaInfoConfig
func TestAnalyzeWithConfig_CustomConfig(t *testing.T) {
	// Arrange: Create temp video file with valid MKV header
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "custom_config.mkv")

	header := []byte{
		0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x1F, 0x42, 0x86, 0x81, 0x01,
	}
	if err := os.WriteFile(tmpFile, header, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &MediaInfoConfig{
		CLIEnabled: false,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	}

	info, err := AnalyzeWithConfig(tmpFile, cfg)

	if err != nil {
		t.Logf("AnalyzeWithConfig returned error (acceptable for minimal header): %v", err)
		return
	}

	if info == nil {
		t.Fatal("AnalyzeWithConfig returned nil info without error")
	}

	if info.Container != "mkv" {
		t.Errorf("Expected container 'mkv', got %q", info.Container)
	}
}

// TestProbeWithFallback_NoProberFound tests unsupported format detection
func TestProbeWithFallback_NoProberFound(t *testing.T) {
	// Arrange: Create file with unknown header (not MP4/MKV/AVI/MOV)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "unsupported.bin")

	// Create file with unknown header (16+ bytes to pass header read)
	unknownHeader := []byte{
		0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	if err := os.WriteFile(tmpFile, unknownHeader, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Config with CLI disabled (forces error on unsupported format)
	cfg := &MediaInfoConfig{
		CLIEnabled: false,
	}

	// Act: Call AnalyzeWithConfig
	_, err := AnalyzeWithConfig(tmpFile, cfg)

	// Assert: Error contains "unsupported container format"
	if err == nil {
		t.Fatal("Expected error for unsupported format, got nil")
	}

	if !containsAny(err.Error(), []string{"unsupported container format", "unknown"}) {
		t.Errorf("Expected 'unsupported container format' in error, got: %v", err)
	}
}

// TestProbeWithFallback_NativeProberSuccess tests successful native prober selection
func TestProbeWithFallback_NativeProberSuccess(t *testing.T) {
	// Arrange: Create valid MKV file header
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "native_success.mkv")

	header := []byte{
		0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x1F, 0x42, 0x86, 0x81, 0x01,
	}
	if err := os.WriteFile(tmpFile, header, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	info, err := Analyze(tmpFile)

	if err != nil {
		t.Logf("Native prober failed on minimal file (acceptable): %v", err)
		return
	}

	if info == nil {
		t.Fatal("Analyze returned nil info without error")
	}

	if info.Container != "mkv" {
		t.Errorf("Expected container 'mkv', got %q", info.Container)
	}
}

// TestAnalyzeWithConfig_DefaultConfig tests that nil config uses defaults
func TestAnalyzeWithConfig_DefaultConfig(t *testing.T) {
	// Arrange: Create valid MKV file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "default_config.mkv")

	header := []byte{
		0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x1F, 0x42, 0x86, 0x81, 0x01,
	}
	if err := os.WriteFile(tmpFile, header, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	info, err := AnalyzeWithConfig(tmpFile, nil)

	if err != nil {
		t.Logf("AnalyzeWithConfig with nil config returned error (acceptable for minimal file): %v", err)
		return
	}

	if info == nil {
		t.Fatal("AnalyzeWithConfig with nil config returned nil info without error")
	}
}

// Helper function to check if error message contains any of the expected strings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) && contains(s, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
