package translation

import _ "embed"

// koreanJAVPromptMarkdown is kept outside Go source so prompt maintenance does
// not require editing a large string-literal slice.
//
//go:embed prompts/korean_jav.md
var koreanJAVPromptMarkdown string

// koreanJAVCompactPromptMarkdown provides the small, principle-based prompt
// used when the editable dictionary mode is enabled.
//
//go:embed prompts/korean_jav_compact.md
var koreanJAVCompactPromptMarkdown string
