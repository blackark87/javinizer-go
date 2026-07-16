package models

// SubtitleMove records the original and new path of a moved subtitle file.
type SubtitleMove struct {
	OriginalPath string
	NewPath      string
	Moved        bool
}
