package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/javinizer/javinizer-go/internal/database"
)

const (
	actressMergeStepInput    = "input"
	actressMergeStepConflict = "conflicts"
	actressMergeStepResult   = "result"

	keyEsc   = "esc"
	keyDown  = "down"
	keyEnter = "enter"
	keySpace = "space"

	resolutionSource = "source"
)

// SetActressRepo sets the actress repository used by the merge modal.
func (m *Model) SetActressRepo(repo *database.ActressRepository) {
	m.actressRepo = repo
}

func (m *Model) setActressMergeFocus(focus int) {
	if focus < 0 || focus > 1 {
		focus = 0
	}
	m.actressMergeFocus = focus
	if focus == 0 {
		m.actressMergeTargetInput.Focus()
		m.actressMergeSourceInput.Blur()
		return
	}
	m.actressMergeSourceInput.Focus()
	m.actressMergeTargetInput.Blur()
}

func (m *Model) resetActressMergeModalState() {
	m.actressMergeStep = actressMergeStepInput
	m.actressMergePreview = nil
	m.actressMergeResult = nil
	m.actressMergeError = ""
	m.actressMergeConflictCursor = 0
	m.actressMergeResolutions = make(map[string]string)
	m.actressMergeTargetInput.SetValue("")
	m.actressMergeSourceInput.SetValue("")
	m.setActressMergeFocus(0)
}

func (m *Model) openActressMergeModal() {
	if m.actressRepo == nil {
		m.AddLog("warn", "Actress merge unavailable: repository not initialized")
		return
	}

	m.showingActressMerge = true
	m.resetActressMergeModalState()
}

func (m *Model) closeActressMergeModal() {
	m.showingActressMerge = false
	m.resetActressMergeModalState()
	m.actressMergeTargetInput.Blur()
	m.actressMergeSourceInput.Blur()
}

func parseActressMergeID(raw string) (uint, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("actress ID is required")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid actress ID: %q", value)
	}
	return uint(parsed), nil
}

func (m *Model) loadActressMergePreview() error {
	if m.actressRepo == nil {
		return fmt.Errorf("actress repository not initialized")
	}

	targetID, err := parseActressMergeID(m.actressMergeTargetInput.Value())
	if err != nil {
		return fmt.Errorf("target ID: %w", err)
	}
	sourceID, err := parseActressMergeID(m.actressMergeSourceInput.Value())
	if err != nil {
		return fmt.Errorf("source ID: %w", err)
	}

	preview, err := m.actressRepo.PreviewMerge(targetID, sourceID)
	if err != nil {
		return err
	}

	m.actressMergePreview = preview
	m.actressMergeResult = nil
	m.actressMergeError = ""
	m.actressMergeConflictCursor = 0
	m.actressMergeResolutions = make(map[string]string, len(preview.DefaultResolutions))
	for field, decision := range preview.DefaultResolutions {
		m.actressMergeResolutions[field] = decision
	}
	for _, conflict := range preview.Conflicts {
		if _, ok := m.actressMergeResolutions[conflict.Field]; !ok {
			m.actressMergeResolutions[conflict.Field] = conflict.DefaultResolution
		}
	}
	m.actressMergeStep = actressMergeStepConflict
	return nil
}

func (m *Model) applyActressMerge() error {
	if m.actressRepo == nil {
		return fmt.Errorf("actress repository not initialized")
	}

	targetID, err := parseActressMergeID(m.actressMergeTargetInput.Value())
	if err != nil {
		return fmt.Errorf("target ID: %w", err)
	}
	sourceID, err := parseActressMergeID(m.actressMergeSourceInput.Value())
	if err != nil {
		return fmt.Errorf("source ID: %w", err)
	}

	result, err := m.actressRepo.Merge(targetID, sourceID, m.actressMergeResolutions)
	if err != nil {
		return err
	}

	m.actressMergeResult = result
	m.actressMergeError = ""
	m.actressMergeStep = actressMergeStepResult
	m.AddLog("info", fmt.Sprintf("Merged actress #%d into #%d", result.MergedFromID, result.MergedActress.ID))
	return nil
}

func (m *Model) getCurrentMergeConflict() *database.ActressMergeConflict {
	if m.actressMergePreview == nil || len(m.actressMergePreview.Conflicts) == 0 {
		return nil
	}
	if m.actressMergeConflictCursor < 0 {
		m.actressMergeConflictCursor = 0
	}
	if m.actressMergeConflictCursor >= len(m.actressMergePreview.Conflicts) {
		m.actressMergeConflictCursor = len(m.actressMergePreview.Conflicts) - 1
	}
	return &m.actressMergePreview.Conflicts[m.actressMergeConflictCursor]
}

func formatConflictValue(value interface{}) string {
	if value == nil {
		return "(empty)"
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "(empty)"
		}
		return v
	default:
		return fmt.Sprintf("%v", value)
	}
}

func handleActressMergeInput(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.actressMergeStep {
	case actressMergeStepInput:
		return handleActressMergeInputStep(m, msg)
	case actressMergeStepConflict:
		return handleActressMergeConflictStep(m, msg)
	case actressMergeStepResult:
		return handleActressMergeResultStep(m, msg)
	default:
		m.actressMergeStep = actressMergeStepInput
		return m, nil
	}
}

func handleActressMergeInputStep(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q":
		m.closeActressMergeModal()
		return m, nil
	case "tab", "up", keyDown:
		if m.actressMergeFocus == 0 {
			m.setActressMergeFocus(1)
		} else {
			m.setActressMergeFocus(0)
		}
		return m, nil
	case keyEnter:
		if m.actressMergeFocus == 0 {
			m.setActressMergeFocus(1)
			return m, nil
		}
		if err := m.loadActressMergePreview(); err != nil {
			m.actressMergeError = normalizeActressMergeError(err)
			m.AddLog("warn", "Actress merge preview failed: "+err.Error())
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.actressMergeFocus == 0 {
		m.actressMergeTargetInput, cmd = m.actressMergeTargetInput.Update(msg)
	} else {
		m.actressMergeSourceInput, cmd = m.actressMergeSourceInput.Update(msg)
	}
	return m, cmd
}

func handleActressMergeConflictStep(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q":
		m.closeActressMergeModal()
		return m, nil
	case "r":
		m.actressMergeStep = actressMergeStepInput
		m.actressMergePreview = nil
		m.actressMergeResult = nil
		m.actressMergeError = ""
		m.setActressMergeFocus(0)
		return m, nil
	case "up", "k":
		if m.actressMergeConflictCursor > 0 {
			m.actressMergeConflictCursor--
		}
		return m, nil
	case keyDown, "j":
		if m.actressMergePreview != nil && m.actressMergeConflictCursor < len(m.actressMergePreview.Conflicts)-1 {
			m.actressMergeConflictCursor++
		}
		return m, nil
	case "t", "h", "left":
		if conflict := m.getCurrentMergeConflict(); conflict != nil {
			m.actressMergeResolutions[conflict.Field] = "target"
		}
		return m, nil
	case "s", "l", "right":
		if conflict := m.getCurrentMergeConflict(); conflict != nil {
			m.actressMergeResolutions[conflict.Field] = resolutionSource
		}
		return m, nil
	case " ", keySpace:
		if conflict := m.getCurrentMergeConflict(); conflict != nil {
			current := m.actressMergeResolutions[conflict.Field]
			if current == resolutionSource {
				m.actressMergeResolutions[conflict.Field] = "target"
			} else {
				m.actressMergeResolutions[conflict.Field] = resolutionSource
			}
		}
		return m, nil
	case keyEnter:
		if err := m.applyActressMerge(); err != nil {
			m.actressMergeError = normalizeActressMergeError(err)
			m.AddLog("warn", "Actress merge failed: "+err.Error())
		}
		return m, nil
	}

	return m, nil
}

func handleActressMergeResultStep(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q", keyEnter:
		m.closeActressMergeModal()
		return m, nil
	case "r":
		keepTarget := strings.TrimSpace(m.actressMergeTargetInput.Value())
		m.resetActressMergeModalState()
		m.actressMergeTargetInput.SetValue(keepTarget)
		m.setActressMergeFocus(1)
		return m, nil
	}

	return m, nil
}

// RenderActressMergeModal renders the actress merge modal overlay.
func RenderActressMergeModal(m *Model) string {
	modalWidth := 78
	if m.width > 0 && m.width-4 < modalWidth {
		modalWidth = m.width - 4
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	modalHeight := 24
	if m.height > 0 && m.height-4 < modalHeight {
		modalHeight = m.height - 4
	}
	if modalHeight < 14 {
		modalHeight = 14
	}

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Width(modalWidth).
		Height(modalHeight)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("63")).
		MarginBottom(1)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196"))

	muted := lipgloss.NewStyle().Faint(true)

	lines := []string{titleStyle.Render("Actress Merge")}
	if m.actressMergeError != "" {
		lines = append(lines, errorStyle.Render("Error: "+m.actressMergeError), "")
	}

	switch m.actressMergeStep {
	case actressMergeStepInput:
		targetLabel := "  Target ID: "
		sourceLabel := "  Source ID: "
		if m.actressMergeFocus == 0 {
			targetLabel = "▸ Target ID: "
		} else {
			sourceLabel = "▸ Source ID: "
		}
		lines = append(lines,
			targetLabel+m.actressMergeTargetInput.View(),
			sourceLabel+m.actressMergeSourceInput.View(),
			"",
			muted.Render("Tab/↑↓ switch field • Enter on Source loads preview • Esc cancel"),
		)

	case actressMergeStepConflict:
		if m.actressMergePreview == nil {
			lines = append(lines, "No merge preview loaded.", "", muted.Render("Press r to go back or Esc to close."))
			break
		}

		lines = append(lines,
			fmt.Sprintf("Merging source #%d into target #%d", m.actressMergePreview.Source.ID, m.actressMergePreview.Target.ID),
			fmt.Sprintf("Conflicts: %d", len(m.actressMergePreview.Conflicts)),
			"",
		)

		if len(m.actressMergePreview.Conflicts) == 0 {
			lines = append(lines,
				"No field conflicts found. Default merge behavior will be used.",
				"",
				muted.Render("Enter apply merge • r edit IDs • Esc cancel"),
			)
			break
		}

		for i, conflict := range m.actressMergePreview.Conflicts {
			cursor := "  "
			if i == m.actressMergeConflictCursor {
				cursor = "▸ "
			}
			decision := m.actressMergeResolutions[conflict.Field]
			if decision == "" {
				decision = conflict.DefaultResolution
			}
			lines = append(lines, fmt.Sprintf("%s%s [%s]", cursor, conflict.Field, decision))
		}

		if conflict := m.getCurrentMergeConflict(); conflict != nil {
			decision := m.actressMergeResolutions[conflict.Field]
			if decision == "" {
				decision = conflict.DefaultResolution
			}
			lines = append(lines,
				"",
				fmt.Sprintf("Field: %s (selected: %s)", conflict.Field, decision),
				"  target: "+formatConflictValue(conflict.TargetValue),
				"  source: "+formatConflictValue(conflict.SourceValue),
			)
		}

		lines = append(lines, "", muted.Render("↑↓ choose field • t/s select value • Space toggle • Enter apply • r edit IDs • Esc cancel"))

	case actressMergeStepResult:
		if m.actressMergeResult == nil {
			lines = append(lines, "Merge finished, but no result is available.", "", muted.Render("r new merge • Esc close"))
			break
		}

		lines = append(lines,
			fmt.Sprintf("Merged actress #%d into #%d", m.actressMergeResult.MergedFromID, m.actressMergeResult.MergedActress.ID),
			fmt.Sprintf("Updated movies: %d", m.actressMergeResult.UpdatedMovies),
			fmt.Sprintf("Conflicts resolved: %d", m.actressMergeResult.ConflictsResolved),
			fmt.Sprintf("Aliases added: %d", m.actressMergeResult.AliasesAdded),
			"",
			muted.Render("r new merge with same target • Enter/Esc close"),
		)
	}

	content := strings.Join(lines, "\n")
	modal := modalStyle.Render(content)

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}

func normalizeActressMergeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, database.ErrActressMergeSameID) {
		return "target and source must be different actress IDs"
	}
	if errors.Is(err, database.ErrActressMergeInvalidID) {
		return "target and source must be positive actress IDs"
	}
	return err.Error()
}
