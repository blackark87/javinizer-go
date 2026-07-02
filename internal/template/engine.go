package template

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	cjkRegex              = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]`)
	conditionalTokenRegex = regexp.MustCompile(`(?i)<IF:[A-Z_]+(?::[a-zA-Z]{2,5})?>|</IF>`)
)

const (
	DefaultMaxTemplateBytes    = 64 * 1024
	DefaultMaxOutputBytes      = 10 * 1024 * 1024
	DefaultMaxConditionalDepth = 32
)

type EngineOptions struct {
	MaxTemplateBytes    int
	MaxOutputBytes      int
	MaxConditionalDepth int
	DefaultLanguage     string
	FallbackLanguages   []string
}

type parsedModifier struct {
	isLanguage       bool
	languageSpec     string
	legacyModifier   string
	rejectedLanguage bool
}

type Engine struct {
	tagPattern         *regexp.Regexp
	conditionalPattern *regexp.Regexp
	options            EngineOptions
}

func NewEngine() *Engine {
	return NewEngineWithOptions(EngineOptions{})
}

func NewEngineWithOptions(opts EngineOptions) *Engine {
	if opts.MaxTemplateBytes <= 0 {
		opts.MaxTemplateBytes = DefaultMaxTemplateBytes
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if opts.MaxConditionalDepth <= 0 {
		opts.MaxConditionalDepth = DefaultMaxConditionalDepth
	}
	opts.DefaultLanguage = normalizeLanguageCode(opts.DefaultLanguage)
	opts.FallbackLanguages = normalizeLanguageList(opts.FallbackLanguages)

	return &Engine{
		tagPattern:         regexp.MustCompile(`(?i)<([A-Z_]+)(?::([^>]+))?>`),
		conditionalPattern: regexp.MustCompile(`(?i)<IF:([A-Z_]+(?::[a-zA-Z]{2,5})??)>(.*?)(?:<ELSE>(.*?))?</IF>`),
		options:            opts,
	}
}

func (e *Engine) Execute(template string, ctx *Context) (string, error) {
	return e.ExecuteWithContext(context.Background(), template, ctx)
}

func (e *Engine) ExecuteWithMaxBytes(tmpl string, ctx *Context, maxBytes int) (string, error) {
	sentinel := "\x00MAXBYTES\x00"
	frameCtx := ctx.Clone()
	frameCtx.Title = sentinel
	frameCtx.OriginalTitle = sentinel

	frame, err := e.Execute(tmpl, frameCtx)
	if err != nil {
		return e.Execute(tmpl, ctx)
	}

	frameBytes := len(frame) - strings.Count(frame, sentinel)*len(sentinel)
	titleBudget := maxBytes - frameBytes
	if titleBudget <= 0 || len(ctx.Title) <= titleBudget {
		return e.Execute(tmpl, ctx)
	}

	truncatedCtx := ctx.Clone()
	truncated := e.TruncateTitleBytes(ctx.Title, titleBudget)
	truncatedCtx.Title = truncated
	if ctx.OriginalTitle == ctx.Title {
		truncatedCtx.OriginalTitle = truncated
	} else {
		truncatedCtx.OriginalTitle = e.TruncateTitleBytes(ctx.OriginalTitle, titleBudget)
	}
	return e.Execute(tmpl, truncatedCtx)
}

func (e *Engine) ExecuteWithContext(execCtx context.Context, template string, ctx *Context) (string, error) {
	if execCtx == nil {
		return "", fmt.Errorf("execution context cannot be nil")
	}
	if ctx == nil {
		return "", fmt.Errorf("context cannot be nil")
	}
	if err := e.checkExecutionContext(execCtx); err != nil {
		return "", err
	}
	if err := e.Validate(template); err != nil {
		return "", err
	}

	result, err := e.processConditionalsWithContext(execCtx, template, ctx)
	if err != nil {
		return "", err
	}
	if err := e.ensureOutputWithinLimit(result); err != nil {
		return "", err
	}

	tagReplacements := make(map[string]string)
	matches := e.tagPattern.FindAllStringSubmatch(result, -1)
	for i, match := range matches {
		if i%25 == 0 {
			if err := e.checkExecutionContext(execCtx); err != nil {
				return "", err
			}
		}
		fullTag := match[0]
		tagName := strings.ToUpper(match[1])
		modifier := ""
		if len(match) > 2 {
			modifier = match[2]
		}
		if _, seen := tagReplacements[fullTag]; !seen {
			value, err := e.resolveTag(tagName, modifier, ctx)
			if err != nil {
				value = ""
			}
			tagReplacements[fullTag] = value
		}
	}

	result = e.tagPattern.ReplaceAllStringFunc(result, func(match string) string {
		return tagReplacements[match]
	})
	if err := e.ensureOutputWithinLimit(result); err != nil {
		return "", err
	}
	if err := e.checkExecutionContext(execCtx); err != nil {
		return "", err
	}
	return result, nil
}

func (e *Engine) processConditionalsWithContext(execCtx context.Context, template string, ctx *Context) (string, error) {
	result := template
	matches := e.conditionalPattern.FindAllStringSubmatch(result, -1)
	blockReplacements := make(map[string]string)

	for i, match := range matches {
		if i%25 == 0 {
			if err := e.checkExecutionContext(execCtx); err != nil {
				return "", err
			}
		}
		fullBlock := match[0]
		rawTag := match[1]
		tagName := strings.ToUpper(rawTag)
		modifier := ""
		if idx := strings.Index(rawTag, ":"); idx != -1 {
			tagName = strings.ToUpper(rawTag[:idx])
			modifier = strings.ToLower(rawTag[idx+1:])
		}
		trueContent := match[2]
		falseContent := ""
		if len(match) > 3 {
			falseContent = match[3]
		}
		value, _ := e.resolveTag(tagName, modifier, ctx)
		if value != "" {
			blockReplacements[fullBlock] = trueContent
		} else {
			blockReplacements[fullBlock] = falseContent
		}
	}

	result = e.conditionalPattern.ReplaceAllStringFunc(result, func(match string) string {
		return blockReplacements[match]
	})
	if err := e.ensureOutputWithinLimit(result); err != nil {
		return "", err
	}
	return result, nil
}

func (e *Engine) Validate(template string) error {
	if len(template) > e.options.MaxTemplateBytes {
		return fmt.Errorf("template size %d exceeds maximum %d bytes", len(template), e.options.MaxTemplateBytes)
	}
	depth := 0
	tokens := conditionalTokenRegex.FindAllString(template, -1)
	for _, token := range tokens {
		if strings.HasPrefix(strings.ToUpper(token), "<IF:") {
			depth++
			if depth > e.options.MaxConditionalDepth {
				return fmt.Errorf("conditional depth %d exceeds maximum %d", depth, e.options.MaxConditionalDepth)
			}
			continue
		}
		depth--
		if depth < 0 {
			return fmt.Errorf("invalid template conditionals: unexpected closing </IF>")
		}
	}
	if depth != 0 {
		return fmt.Errorf("invalid template conditionals: unclosed <IF> block")
	}
	return nil
}

func (e *Engine) ensureOutputWithinLimit(output string) error {
	if len(output) > e.options.MaxOutputBytes {
		return fmt.Errorf("rendered template size %d exceeds maximum %d bytes", len(output), e.options.MaxOutputBytes)
	}
	return nil
}

func (e *Engine) checkExecutionContext(execCtx context.Context) error {
	if err := execCtx.Err(); err != nil {
		return fmt.Errorf("template execution canceled: %w", err)
	}
	return nil
}

func (e *Engine) resolveTag(tagName, modifier string, ctx *Context) (string, error) {
	parsed := e.parseModifier(tagName, modifier)

	switch tagName {
	case "ID":
		value := ctx.ID
		if modifier != "" {
			return e.applyCaseModifier(value, modifier), nil
		}
		return value, nil
	case "CONTENTID":
		value := ctx.ContentID
		if modifier != "" {
			return e.applyCaseModifier(value, modifier), nil
		}
		return value, nil
	case "TITLE":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			value := e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx)
			if parsed.legacyModifier != "" {
				return e.truncate(value, parsed.legacyModifier), nil
			}
			return value, nil
		}
		value := ctx.Title
		if modifier != "" {
			return e.truncate(value, modifier), nil
		}
		return value, nil
	case "ORIGINALTITLE":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.OriginalTitle, nil
	case "YEAR":
		if ctx.ReleaseDate != nil {
			return fmt.Sprintf("%d", ctx.ReleaseDate.Year()), nil
		}
		if ctx.ReleaseYear > 0 {
			return fmt.Sprintf("%d", ctx.ReleaseYear), nil
		}
		return "", nil
	case "RELEASEDATE":
		if ctx.ReleaseDate != nil {
			if modifier != "" {
				return e.formatDate(ctx.ReleaseDate, modifier), nil
			}
			return ctx.ReleaseDate.Format("2006-01-02"), nil
		}
		return "", nil
	case "RUNTIME":
		if ctx.Runtime > 0 {
			return fmt.Sprintf("%d", ctx.Runtime), nil
		}
		return "", nil
	case "DIRECTOR":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Director, nil
	case "DESCRIPTION":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Description, nil
	case "STUDIO", "MAKER":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Maker, nil
	case "LABEL":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Label, nil
	case "SERIES", "SET":
		if e.isTranslatableTag(tagName) && !parsed.rejectedLanguage {
			return e.resolveTranslatedTag(tagName, parsed.languageSpec, ctx), nil
		}
		return ctx.Series, nil
	case "ACTORS", "ACTRESSES":
		if len(ctx.Actresses) > 0 {
			if ctx.GroupActress && len(ctx.Actresses) > 1 {
				groupName := ctx.GroupActressName
				if groupName == "" {
					groupName = "@Group"
				}
				return groupName, nil
			}
			delimiter := ", "
			if modifier != "" {
				delimiter = modifier
			}
			if len(e.languageCandidates("", ctx)) > 0 {
				return e.resolveActressNamesWithDelimiter(delimiter, ctx), nil
			}
			return strings.Join(ctx.formatActressNames(), delimiter), nil
		}
		return "", nil
	case "ACTRESS", "ACTORNAME", "ACTRESSNAME":
		if len(e.languageCandidates("", ctx)) > 0 {
			if name := e.resolveActressName(0, ctx); name != "" {
				return name, nil
			}
		}
		if ctx.ActressName != "" {
			return ctx.ActressName, nil
		}
		if len(ctx.ActressDetails) > 0 {
			return ctx.formatActressName(ctx.ActressDetails[0]), nil
		}
		if len(ctx.Actresses) > 0 {
			return ctx.Actresses[0], nil
		}
		return "", nil
	case "GENRES":
		if len(ctx.Genres) > 0 {
			delimiter := ", "
			if modifier != "" {
				delimiter = modifier
			}
			return strings.Join(ctx.Genres, delimiter), nil
		}
		return "", nil
	case "FILENAME":
		if ctx.OriginalFilename != "" {
			return ctx.OriginalFilename, nil
		}
		return ctx.SourceFilename, nil
	case "SOURCEPATH":
		return ctx.SourcePath, nil
	case "SOURCEDIR":
		return ctx.SourceDir, nil
	case "SOURCEFOLDER":
		return ctx.SourceFolder, nil
	case "SOURCEPARENT":
		return ctx.SourceParent, nil
	case "SOURCEFILE":
		return ctx.SourceFile, nil
	case "SOURCEFILENAME":
		return ctx.SourceFilename, nil
	case "SOURCEEXT", "SOURCEEXTENSION":
		return ctx.SourceExtension, nil
	case "INDEX":
		if ctx.Index > 0 {
			if modifier != "" {
				format := fmt.Sprintf("%%0%sd", modifier)
				return fmt.Sprintf(format, ctx.Index), nil
			}
			return fmt.Sprintf("%d", ctx.Index), nil
		}
		return "", nil
	case "FIRSTNAME":
		return ctx.FirstName, nil
	case "LASTNAME":
		return ctx.LastName, nil
	case "RESOLUTION":
		info := ctx.GetMediaInfo()
		if info != nil {
			return info.GetResolution(), nil
		}
		return "", nil
	case "PART", "DISC":
		if ctx.PartNumber > 0 {
			if modifier != "" {
				format := fmt.Sprintf("%%0%sd", modifier)
				return fmt.Sprintf(format, ctx.PartNumber), nil
			}
			return fmt.Sprintf("%d", ctx.PartNumber), nil
		}
		return "", nil
	case "PARTSUFFIX":
		return ctx.PartSuffix, nil
	case "RATING":
		if ctx.Rating > 0 {
			return fmt.Sprintf("%.1f", ctx.Rating), nil
		}
		return "", nil
	case "MULTIPART":
		if ctx.IsMultiPart {
			return "true", nil
		}
		return "", nil
	case "VR":
		if ctx.IsVR() {
			return "true", nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("unknown tag: %s", tagName)
	}
}

func (e *Engine) TruncateTitle(title string, maxLen int) string {
	if maxLen <= 0 || len(title) <= maxLen {
		return title
	}
	marker := "..."
	if e.containsCJK(title) {
		if maxLen > 3 {
			runes := []rune(title)
			if len(runes) > maxLen-3 {
				return string(runes[:maxLen-3]) + marker
			}
		}
		return title
	}
	runes := []rune(title)
	if maxLen > 3 {
		if len(runes) > maxLen-3 {
			truncated := string(runes[:maxLen-3])
			if lastSpace := strings.LastIndex(truncated, " "); lastSpace > 0 {
				return truncated[:lastSpace] + marker
			}
			return truncated + marker
		}
		return title
	}
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return title
}

func (e *Engine) TruncateTitleBytes(title string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(title) <= maxBytes {
		return title
	}
	marker := "..."
	markerReserve := 3
	if maxBytes <= markerReserve {
		runes := []rune(title)
		currentBytes := 0
		for i, r := range runes {
			runeSize := len(string(r))
			if currentBytes+runeSize > maxBytes {
				if i == 0 {
					return ""
				}
				return string(runes[:i])
			}
			currentBytes += runeSize
		}
		return title
	}
	budget := maxBytes - markerReserve
	runes := []rune(title)
	currentBytes := 0
	endIdx := 0
	for i, r := range runes {
		runeSize := len(string(r))
		if currentBytes+runeSize > budget {
			break
		}
		currentBytes += runeSize
		endIdx = i + 1
	}
	if endIdx == 0 {
		return marker
	}
	truncated := string(runes[:endIdx])
	if !e.containsCJK(title) {
		if lastSpacePos := strings.LastIndex(truncated, " "); lastSpacePos > 0 {
			truncated = truncated[:lastSpacePos]
		}
	}
	return strings.TrimRight(truncated, " ") + marker
}

func (e *Engine) ValidatePathLength(path string, maxLen int) error {
	if maxLen <= 0 {
		return nil
	}
	if len(path) > maxLen {
		return fmt.Errorf("path length %d exceeds limit %d", len(path), maxLen)
	}
	return nil
}

func (e *Engine) containsCJK(s string) bool {
	return cjkRegex.MatchString(s)
}

func (e *Engine) truncate(s string, maxLenStr string) string {
	maxLen, err := strconv.Atoi(maxLenStr)
	if err != nil || maxLen <= 0 {
		return s
	}
	return e.TruncateTitle(s, maxLen)
}

func (e *Engine) formatDate(date *time.Time, pattern string) string {
	pattern = strings.ReplaceAll(pattern, "YYYY", "2006")
	pattern = strings.ReplaceAll(pattern, "YY", "06")
	pattern = strings.ReplaceAll(pattern, "MM", "01")
	pattern = strings.ReplaceAll(pattern, "DD", "02")
	pattern = strings.ReplaceAll(pattern, "HH", "15")
	pattern = strings.ReplaceAll(pattern, "mm", "04")
	pattern = strings.ReplaceAll(pattern, "ss", "05")
	return date.Format(pattern)
}

func (e *Engine) applyCaseModifier(value, modifier string) string {
	switch strings.ToUpper(modifier) {
	case "UPPERCASE", "UPPER":
		return strings.ToUpper(value)
	case "LOWERCASE", "LOWER":
		return strings.ToLower(value)
	default:
		return value
	}
}

func normalizeLanguageList(langs []string) []string {
	if len(langs) == 0 {
		return nil
	}
	out := make([]string, 0, len(langs))
	seen := map[string]struct{}{}
	for _, lang := range langs {
		norm := normalizeLanguageCode(lang)
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}

func (e *Engine) parseModifier(tagName, modifier string) parsedModifier {
	if modifier == "" {
		return parsedModifier{}
	}
	if normalized := normalizeLanguageCode(modifier); normalized != "" {
		return parsedModifier{isLanguage: true, languageSpec: normalized}
	}
	if strings.Contains(modifier, "|") {
		parts := strings.Split(modifier, "|")
		valid := true
		for _, part := range parts {
			if normalizeLanguageCode(part) == "" {
				valid = false
				break
			}
		}
		if valid {
			return parsedModifier{isLanguage: true, languageSpec: modifier}
		}
	}
	if tagName == "TITLE" && e.isNumericModifier(modifier) {
		return parsedModifier{legacyModifier: modifier}
	}
	if e.isTranslatableTag(tagName) && e.looksLikeLanguageSpec(modifier) {
		return parsedModifier{rejectedLanguage: true}
	}
	return parsedModifier{legacyModifier: modifier}
}

func (e *Engine) isNumericModifier(modifier string) bool {
	if modifier == "" {
		return false
	}
	n, err := strconv.Atoi(modifier)
	return err == nil && n > 0
}

func (e *Engine) looksLikeLanguageSpec(modifier string) bool {
	if modifier == "" {
		return false
	}
	if strings.Contains(modifier, "|") {
		return true
	}
	trimmed := strings.TrimSpace(modifier)
	if idx := strings.IndexAny(trimmed, "-_"); idx > 0 {
		prefix := trimmed[:idx]
		if len(prefix) >= 2 && len(prefix) <= 3 {
			for _, r := range strings.ToLower(prefix) {
				if r < 'a' || r > 'z' {
					return false
				}
			}
			return true
		}
		return false
	}
	lower := strings.ToLower(trimmed)
	if len(lower) >= 2 && len(lower) <= 3 {
		for _, r := range lower {
			if r < 'a' || r > 'z' {
				return false
			}
		}
		return true
	}
	return false
}

func (e *Engine) isTranslatableTag(tagName string) bool {
	switch tagName {
	case "TITLE", "ORIGINALTITLE", "DIRECTOR", "MAKER", "STUDIO", "LABEL", "SERIES", "SET", "DESCRIPTION":
		return true
	default:
		return false
	}
}

func (e *Engine) languageCandidates(explicitLang string, ctx *Context) []string {
	var candidates []string
	seen := map[string]struct{}{}
	addCandidate := func(lang string) {
		lang = normalizeLanguageCode(lang)
		if lang == "" {
			return
		}
		if _, exists := seen[lang]; exists {
			return
		}
		seen[lang] = struct{}{}
		candidates = append(candidates, lang)
	}
	if explicitLang != "" {
		for _, lang := range strings.Split(explicitLang, "|") {
			addCandidate(lang)
		}
	}
	if ctx.DefaultLanguage != "" {
		addCandidate(ctx.DefaultLanguage)
	}
	if e.options.DefaultLanguage != "" {
		addCandidate(e.options.DefaultLanguage)
	}
	for _, lang := range e.options.FallbackLanguages {
		addCandidate(lang)
	}
	return candidates
}

func (e *Engine) resolveTranslatedTag(tagName, explicitLang string, ctx *Context) string {
	for _, lang := range e.languageCandidates(explicitLang, ctx) {
		if value := e.translationFieldValue(tagName, lang, ctx); value != "" {
			return value
		}
	}
	return e.resolveBaseTag(tagName, ctx)
}

// resolveActressNamesWithDelimiter joins actress names resolved via the global
// translation languages (no explicit per-tag language spec is supported).
func (e *Engine) resolveActressNamesWithDelimiter(delimiter string, ctx *Context) string {
	if len(ctx.Actresses) == 0 && len(ctx.ActressDetails) == 0 {
		return ""
	}
	names := make([]string, 0, max(len(ctx.Actresses), len(ctx.ActressDetails)))
	count := len(ctx.ActressDetails)
	if len(ctx.Actresses) > count {
		count = len(ctx.Actresses)
	}
	for i := 0; i < count; i++ {
		if name := e.resolveActressName(i, ctx); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, delimiter)
}

func (e *Engine) resolveActressName(index int, ctx *Context) string {
	primaryLang := ""
	for _, lang := range e.languageCandidates("", ctx) {
		if primaryLang == "" {
			primaryLang = lang
		}
		if name := e.translatedActressName(lang, index, ctx); name != "" {
			return name
		}
	}
	if index < len(ctx.ActressDetails) {
		detail := ctx.ActressDetails[index]
		// Language-shaped name (JapaneseName for ja, CJK rejection for en)
		if name := ctx.formatActressNameForLanguage(detail, primaryLang); name != "" {
			return name
		}
		if rawName := ctx.formatActressName(detail); rawName != "" {
			return rawName
		}
	}
	// Fall back to raw Actresses slice (covers case where ActressDetails is absent)
	if index < len(ctx.Actresses) {
		if rawName := ctx.Actresses[index]; rawName != "" {
			return rawName
		}
	}
	return ""
}

func (e *Engine) translatedActressName(lang string, index int, ctx *Context) string {
	if ctx.Translations == nil {
		return ""
	}
	translation, ok := ctx.Translations[lang]
	if !ok || index < 0 || index >= len(translation.Actresses) {
		return ""
	}
	return strings.TrimSpace(translation.Actresses[index])
}

func (e *Engine) resolveBaseTag(tagName string, ctx *Context) string {
	switch tagName {
	case "TITLE":
		return ctx.Title
	case "ORIGINALTITLE":
		return ctx.OriginalTitle
	case "DIRECTOR":
		return ctx.Director
	case "MAKER", "STUDIO":
		return ctx.Maker
	case "LABEL":
		return ctx.Label
	case "SERIES", "SET":
		return ctx.Series
	case "DESCRIPTION":
		return ctx.Description
	default:
		return ""
	}
}

func (e *Engine) translationFieldValue(tagName, lang string, ctx *Context) string {
	if ctx.Translations == nil {
		return ""
	}
	translation, ok := ctx.Translations[lang]
	if !ok {
		return ""
	}
	switch tagName {
	case "TITLE":
		return translation.Title
	case "ORIGINALTITLE":
		return translation.OriginalTitle
	case "DIRECTOR":
		return translation.Director
	case "MAKER", "STUDIO":
		return translation.Maker
	case "LABEL":
		return translation.Label
	case "SERIES", "SET":
		return translation.Series
	case "DESCRIPTION":
		return translation.Description
	default:
		return ""
	}
}
