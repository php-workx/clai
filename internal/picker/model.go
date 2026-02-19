package picker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/runger/clai/internal/config"
)

// debounceInterval is the delay after the last keystroke before triggering a fetch.
const debounceInterval = 100 * time.Millisecond

// Layout controls the visual arrangement of list items.
type Layout int

const (
	// LayoutTopDown renders items top-to-bottom in array order (default).
	LayoutTopDown Layout = iota
	// LayoutBottomUp renders items bottom-to-top (newest at bottom, closest
	// to the input field) with padding above when items don't fill the screen.
	LayoutBottomUp
)

// pickerState represents the current state of the picker's state machine.
type pickerState int

const (
	stateIdle      pickerState = iota // Initial state before first fetch
	stateLoading                      // Fetch in progress
	stateLoaded                       // Items loaded successfully (len > 0)
	stateEmpty                        // Fetch succeeded but returned 0 items
	stateError                        // Fetch failed
	stateCancelled                    // User cancelled (Esc)
)

// fetchDoneMsg is sent when an async Provider.Fetch completes.
type fetchDoneMsg struct {
	err       error
	items     []Item
	requestID uint64
	atEnd     bool
}

// debounceMsg fires after the debounce timer expires.
type debounceMsg struct {
	id uint64 // Must match current requestID to be accepted
}

// initMsg is sent by Init() to trigger the first fetch via Update(),
// ensuring state mutations are visible to the Bubble Tea runtime.
type initMsg struct{}

// clipboardMsg is sent after a clipboard copy attempt completes.
type clipboardMsg struct{ err error }

// copiedClearMsg clears the "Copied!" indicator after a delay.
type copiedClearMsg struct{}

// copiedFeedbackDuration is how long the "Copied!" indicator stays visible.
const copiedFeedbackDuration = 1500 * time.Millisecond

// Model is the Bubble Tea model for the history picker TUI.
// It must be exported so that cmd/clai-picker can use it.
type Model struct {
	err         error
	provider    Provider
	cancelFetch context.CancelFunc
	result      string
	tabs        []config.TabDef
	items       []Item
	textInput   textinput.Model
	debounceID  uint64
	requestID   uint64
	state       pickerState
	activeTab   int
	selection   int
	offset      int
	width       int
	height      int
	layout      Layout
	atEnd       bool
	copied      bool
}

// NewModel creates a new picker Model.
func NewModel(tabs []config.TabDef, provider Provider) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = queryStyle
	ti.Placeholder = "type to filter..."
	ti.Focus()
	return Model{
		state:     stateIdle,
		tabs:      tabs,
		activeTab: 0,
		selection: -1,
		provider:  provider,
		textInput: ti,
	}
}

// WithQuery returns a copy of the Model with the initial query set.
func (m Model) WithQuery(q string) Model { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	m.textInput.SetValue(q)
	m.textInput.CursorEnd()
	return m
}

// WithLayout returns a copy of the Model with the given layout.
func (m Model) WithLayout(l Layout) Model { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	m.layout = l
	return m
}

// Layout returns the current layout mode (top-down or bottom-up).
func (m Model) Layout() Layout { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	return m.layout
}

// Result returns the selected command string, or "" if cancelled.
func (m Model) Result() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	return m.result
}

// IsCancelled returns true if the user cancelled the picker (e.g., with Esc).
func (m Model) IsCancelled() bool { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	return m.state == stateCancelled
}

// Init implements tea.Model. It sends an initMsg so that the first fetch
// is triggered through Update, where state mutations are properly captured.
func (m Model) Init() tea.Cmd { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	return tea.Batch(
		textinput.Blink,
		func() tea.Msg { return initMsg{} },
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textInput.Width = m.contentWidth() - 4 // account for prompt prefix and padding
		return m, nil

	case fetchDoneMsg:
		return m.handleFetchDone(msg)

	case debounceMsg:
		return m.handleDebounce(msg)

	case initMsg:
		return m, m.startFetch() //nolint:gocritic // evalOrder: bubbletea Update pattern returns cmd before model

	case clipboardMsg:
		if msg.err == nil {
			m.copied = true
			return m, tea.Tick(copiedFeedbackDuration, func(time.Time) tea.Msg {
				return copiedClearMsg{}
			})
		}
		return m, nil

	case copiedClearMsg:
		m.copied = false
		return m, nil
	}

	// Forward to textinput for cursor blink and other internal messages.
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	switch msg.Type {
	case tea.KeyEsc:
		m.state = stateCancelled
		m.cancelInflight()
		return m, tea.Quit

	case tea.KeyCtrlC:
		return m.handleCopy()

	case tea.KeyCtrlU:
		// Clear the query and refresh results immediately.
		if m.textInput.Value() == "" {
			return m, nil
		}
		m.textInput.SetValue("")
		m.textInput.CursorEnd()
		m.offset = 0
		return m, m.startFetch() //nolint:gocritic // evalOrder: bubbletea Update pattern returns cmd before model

	case tea.KeyEnter:
		return m.handleSelect()

	case tea.KeyUp:
		m.moveSelection(-1)
		return m, nil

	case tea.KeyDown:
		m.moveSelection(+1)
		return m, nil

	case tea.KeyRight:
		return m.handleRightRefineKey()

	case tea.KeyTab:
		return m.handleTabSwitch()
	}

	return m.handleTextInput(msg)
}

// handleRightRefineKey replaces the query with the currently selected item and
// triggers a debounced fetch. This enables a fast "select then refine" flow.
func (m Model) handleRightRefineKey() (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if m.selection < 0 || m.selection >= len(m.items) {
		return m, nil
	}

	query := ValidateUTF8(StripANSI(m.items[m.selection].Value))
	if query == "" || m.textInput.Value() == query {
		return m, nil
	}

	m.textInput.SetValue(query)
	m.textInput.CursorEnd()
	m.offset = 0
	return m, m.startDebounce() //nolint:gocritic // evalOrder: bubbletea Update pattern returns cmd before model
}

// handleCopy copies the selected item to the clipboard.
func (m Model) handleCopy() (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if m.selection >= 0 && m.selection < len(m.items) {
		return m, copyToClipboard(m.items[m.selection].Value)
	}
	return m, nil
}

// handleSelect accepts the current selection and quits.
func (m Model) handleSelect() (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if m.selection >= 0 && m.selection < len(m.items) {
		m.result = m.items[m.selection].Value
	}
	m.cancelInflight()
	return m, tea.Quit
}

// moveSelection moves the selection cursor by delta, respecting layout direction.
// A negative delta means "up" visually; positive means "down" visually.
func (m *Model) moveSelection(delta int) {
	if m.state == stateLoading {
		return
	}
	// In bottom-up layout, visual "up" increases the index.
	if m.layout == LayoutBottomUp {
		delta = -delta
	}
	next := m.selection + delta
	if next >= 0 && next < len(m.items) {
		m.selection = next
	}
}

// handleTabSwitch cycles to the next tab if multiple tabs exist.
func (m Model) handleTabSwitch() (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if len(m.tabs) > 1 {
		m.activeTab = (m.activeTab + 1) % len(m.tabs)
		m.offset = 0
		return m, m.startFetch() //nolint:gocritic // evalOrder: bubbletea Update pattern returns cmd before model
	}
	return m, nil
}

// handleTextInput delegates to the text input widget and triggers a
// debounced search if the query changed.
func (m Model) handleTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	prevQuery := m.textInput.Value()
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)

	if m.textInput.Value() != prevQuery {
		m.offset = 0
		return m, tea.Batch(cmd, m.startDebounce())
	}
	return m, cmd
}

// handleFetchDone processes the result of an async fetch.
func (m Model) handleFetchDone(msg fetchDoneMsg) (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	// Discard stale responses.
	if msg.requestID != m.requestID {
		return m, nil
	}

	if msg.err != nil {
		m.state = stateError
		m.err = msg.err
		m.items = nil
		m.selection = -1
		return m, nil
	}

	items := msg.items
	// Always apply a local substring filter. This keeps behavior consistent
	// across providers (history + suggestions) and allows matching anywhere
	// within the command text.
	if q := strings.TrimSpace(m.textInput.Value()); q != "" {
		qLower := strings.ToLower(q)
		filtered := make([]Item, 0, len(items))
		for _, it := range items {
			// Filter against the raw command value (the thing we'd insert),
			// not the decorated display text.
			val := strings.ToLower(StripANSI(it.Value))
			if strings.Contains(val, qLower) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	m.items = items
	m.atEnd = msg.atEnd

	if len(m.items) == 0 {
		m.state = stateEmpty
		m.selection = -1
	} else {
		m.state = stateLoaded
		m.clampSelection()
	}

	return m, nil
}

// handleDebounce fires the fetch if the debounce timer is still current.
func (m Model) handleDebounce(msg debounceMsg) (tea.Model, tea.Cmd) { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if msg.id != m.debounceID {
		return m, nil // Stale debounce timer; ignore.
	}
	return m, m.startFetch() //nolint:gocritic // evalOrder: bubbletea Update pattern returns cmd before model
}

// startDebounce increments the debounce counter and returns a tea.Tick
// command that fires after debounceInterval.
func (m *Model) startDebounce() tea.Cmd {
	m.debounceID++
	id := m.debounceID
	return tea.Tick(debounceInterval, func(time.Time) tea.Msg {
		return debounceMsg{id: id}
	})
}

// startFetch cancels any in-flight fetch, increments requestID, and
// returns a tea.Cmd that calls the provider.
func (m *Model) startFetch() tea.Cmd {
	m.cancelInflight()
	m.requestID++
	m.state = stateLoading

	reqID := m.requestID
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFetch = cancel

	tab := m.currentTab()
	req := Request{
		RequestID: reqID,
		Query:     m.textInput.Value(),
		TabID:     tab.ID,
		Options:   tab.Args,
		Limit:     m.listHeight(),
		Offset:    m.offset,
	}

	p := m.provider
	return func() tea.Msg {
		resp, err := p.Fetch(ctx, req)
		if err != nil {
			return fetchDoneMsg{requestID: reqID, err: err}
		}
		return fetchDoneMsg{
			requestID: reqID,
			items:     resp.Items,
			atEnd:     resp.AtEnd,
		}
	}
}

// cancelInflight cancels any in-progress fetch context.
func (m *Model) cancelInflight() {
	if m.cancelFetch != nil {
		m.cancelFetch()
		m.cancelFetch = nil
	}
}

// copyToClipboard returns a tea.Cmd that writes text to the system clipboard.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("pbcopy")
		case "linux":
			if path, err := exec.LookPath("xclip"); err == nil {
				cmd = exec.Command(path, "-selection", "clipboard") //nolint:gosec // G204: path from LookPath
			} else if path, err := exec.LookPath("xsel"); err == nil {
				cmd = exec.Command(path, "--clipboard", "--input") //nolint:gosec // G204: path from LookPath
			} else {
				return clipboardMsg{err: fmt.Errorf("no clipboard tool found")}
			}
		default:
			return clipboardMsg{err: fmt.Errorf("unsupported OS: %s", runtime.GOOS)}
		}
		cmd.Stdin = strings.NewReader(text)
		return clipboardMsg{err: cmd.Run()}
	}
}

// clampSelection ensures the selection index is within bounds.
func (m *Model) clampSelection() {
	if len(m.items) == 0 {
		m.selection = -1
		return
	}
	if m.selection < 0 {
		m.selection = 0
	}
	if m.selection >= len(m.items) {
		m.selection = len(m.items) - 1
	}
}

// currentTab returns the active TabDef.
func (m Model) currentTab() config.TabDef { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab]
	}
	return config.TabDef{ID: "default", Label: "Default"}
}

// listHeight returns the number of visible list rows (terminal height minus
// header and footer).
func (m Model) listHeight() int { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	// 1 row for tab bar, 1 row for query line, 1 row for newlines between sections,
	// 1 row for footer hints, 2 rows for top+bottom padding.
	chrome := 6
	if m.layout == LayoutBottomUp {
		chrome++ // +1 for separator line between items and query
	}
	h := m.height - chrome
	if h < 1 {
		h = 20 // Sensible default before first WindowSizeMsg
	}
	return h
}

// contentWidth returns the usable width inside the padded container.
func (m Model) contentWidth() int { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	w := m.width - viewPadX*2
	if w < 1 {
		w = 40
	}
	return w
}

// --- View rendering ---

var (
	activeTabStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	inactiveTabStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	normalStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	matchStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	matchSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	queryStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	truncStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	hintStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
)

// Horizontal padding applied to the entire view for breathing room.
const viewPadX = 2

// View implements tea.Model.
func (m Model) View() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	var b strings.Builder

	// Tab bar
	b.WriteString(m.viewTabBar())
	b.WriteRune('\n')

	// Main content area
	b.WriteString(m.viewContent())
	b.WriteRune('\n')

	// Separator between items and query (BottomUp only)
	if m.layout == LayoutBottomUp {
		b.WriteString(dimStyle.Render(strings.Repeat("─", m.contentWidth())))
		b.WriteRune('\n')
	}

	// Footer hints (always above the query).
	b.WriteString(m.viewFooter())
	b.WriteRune('\n')

	// Query line
	b.WriteString(m.viewQuery())

	// Wrap in a padded container for breathing room around window borders.
	return lipgloss.NewStyle().
		PaddingLeft(viewPadX).
		PaddingRight(viewPadX).
		PaddingTop(1).
		PaddingBottom(1).
		Render(b.String())
}

// viewTabBar renders the tab bar.
func (m Model) viewTabBar() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	var parts []string
	for i, tab := range m.tabs {
		if i == m.activeTab {
			label := " ▸ " + tab.Label + " "
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			label := "   " + tab.Label + " "
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	bar := strings.Join(parts, " ")
	if len(m.tabs) > 1 {
		bar += hintStyle.Render("  " + tabSwitchHintLabel())
	}
	return bar
}

func (m Model) viewFooter() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	lines := m.footerDetailLines()
	parts := []string{
		"Enter accept",
		"Ctrl+U delete",
		"Esc cancel",
	}
	if len(m.tabs) > 1 {
		parts = append(parts, tabSwitchHintLabel())
	}
	if m.state == stateLoaded && len(m.items) > 0 {
		parts = append(parts, rightRefineHintLabel())
	}
	lines = append(lines, dimStyle.Render(strings.Join(parts, " · ")))
	return strings.Join(lines, "\n")
}

func (m Model) footerDetailLines() []string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	if m.state != stateLoaded || len(m.items) == 0 || m.selection < 0 || m.selection >= len(m.items) {
		return nil
	}
	details := m.items[m.selection].Details
	if len(details) == 0 {
		return nil
	}
	if len(details) > 2 {
		details = details[:2]
	}
	cw := m.contentWidth()
	lines := make([]string, 0, len(details))
	for _, d := range details {
		lines = append(lines, dimStyle.Render(truncateFooterDetail(d, cw)))
	}
	return lines
}

func truncateFooterDetail(detail string, width int) string {
	if width <= 0 || lipgloss.Width(detail) <= width {
		return detail
	}
	truncateWidth := width - 2
	if truncateWidth < 0 {
		truncateWidth = 0
	}
	return MiddleTruncate(detail, truncateWidth)
}

// viewContent renders the item list or a status message.
func (m Model) viewContent() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	var text string
	switch m.state {
	case stateIdle, stateLoading:
		text = dimStyle.Render("Loading...")
	case stateEmpty:
		text = dimStyle.Render("No matches")
	case stateError:
		msg := "Error"
		if m.err != nil {
			msg = fmt.Sprintf("Error: %s", m.err)
		}
		text = errorStyle.Render(msg)
	case stateCancelled:
		text = dimStyle.Render("Cancelled")
	case stateLoaded:
		return m.viewList() // viewList handles its own padding
	default:
		return ""
	}

	// For non-list states, bottom-align if needed.
	if m.layout == LayoutBottomUp {
		h := m.listHeight()
		pad := h - 1 // status message is 1 line
		if pad > 0 {
			return strings.Repeat("\n", pad) + text
		}
	}
	return text
}

// viewList renders the item list with selection marker.
func (m Model) viewList() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	maxItems := m.listHeight()
	n := len(m.items)
	if n > maxItems {
		n = maxItems
	}

	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, m.renderListLine(i))
	}

	if m.layout == LayoutBottomUp {
		lines = bottomAlignLines(reverseLines(lines), maxItems)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderListLine(i int) string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	query := m.textInput.Value()
	display := m.prepareDisplayForLine(i)
	base, hl, prefix := m.lineStyles(i, strings.HasPrefix(display, "[G] "))
	cmdPart, metaPart := splitDisplayMeta(display)
	line := base.Render(prefix) + renderItem(cmdPart, query, base, hl)
	if metaPart != "" {
		line += dimStyle.Render(metaPart)
	}
	if i == m.selection {
		line += hintStyle.Render("  " + rightRefineHintLabel())
	}
	return line
}

func (m Model) prepareDisplayForLine(i int) string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	display := StripANSI(m.items[i].displayText())
	maxDisplayWidth := m.contentWidth() - lineReservedWidth(i == m.selection)
	if maxDisplayWidth < 0 {
		maxDisplayWidth = 0
	}
	if lipgloss.Width(display) <= maxDisplayWidth {
		return display
	}
	truncateWidth := maxDisplayWidth - 2
	if truncateWidth < 0 {
		truncateWidth = 0
	}
	return MiddleTruncate(display, truncateWidth)
}

func lineReservedWidth(selected bool) int {
	width := 2 // prefix: "> " or "  "
	if selected {
		width += lipgloss.Width("  " + rightRefineHintLabel())
	}
	return width
}

//nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
func (m Model) lineStyles(i int, isGlobalFallback bool) (base, highlight lipgloss.Style, prefix string) {
	if i == m.selection {
		return selectedStyle, matchSelectedStyle, "> "
	}
	if isGlobalFallback {
		return dimStyle, matchStyle, "  "
	}
	return normalStyle, matchStyle, "  "
}

func splitDisplayMeta(display string) (cmd, meta string) {
	if idx := strings.Index(display, "  · "); idx >= 0 {
		return display[:idx], display[idx:]
	}
	return display, ""
}

func reverseLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func bottomAlignLines(lines []string, maxItems int) []string {
	pad := maxItems - len(lines)
	if pad <= 0 {
		return lines
	}
	padding := make([]string, pad)
	return append(padding, lines...)
}

// ellipsis is the truncation marker used by MiddleTruncate.
const ellipsis = "\u2026"

// renderItem renders a display string with styled truncation ellipsis and
// query highlighting. If the display contains an ellipsis from MiddleTruncate,
// the ellipsis is rendered with truncStyle while the surrounding text gets
// query highlighting.
func renderItem(display, query string, base, hl lipgloss.Style) string { //nolint:gocritic // hugeParam: lipgloss.Style is idiomatically passed by value
	parts := strings.SplitN(display, ellipsis, 2)
	if len(parts) == 2 {
		return highlightQuery(parts[0], query, base, hl) +
			truncStyle.Render(" "+ellipsis+" ") +
			highlightQuery(parts[1], query, base, hl)
	}
	return highlightQuery(display, query, base, hl)
}

// highlightQuery renders display text with occurrences of query highlighted.
// Matching is case-insensitive. Non-matching segments use base style;
// matching segments use highlight style.
//
//nolint:gocritic // hugeParam: lipgloss.Style is idiomatically passed by value
func highlightQuery(display, query string, base, highlight lipgloss.Style) string {
	if query == "" {
		return base.Render(display)
	}
	lower := strings.ToLower(display)
	lowerQuery := strings.ToLower(query)

	var b strings.Builder
	pos := 0
	for {
		idx := strings.Index(lower[pos:], lowerQuery)
		if idx == -1 {
			b.WriteString(base.Render(display[pos:]))
			break
		}
		if idx > 0 {
			b.WriteString(base.Render(display[pos : pos+idx]))
		}
		matchEnd := pos + idx + len(lowerQuery)
		b.WriteString(highlight.Render(display[pos+idx : matchEnd]))
		pos = matchEnd
	}
	return b.String()
}

// viewQuery renders the query input line.
func (m Model) viewQuery() string { //nolint:gocritic // hugeParam: bubbletea tea.Model requires value receiver
	q := m.textInput.View()
	if m.copied {
		q += "  " + dimStyle.Render("Copied!")
	}
	return q
}

func rightRefineHintLabel() string {
	if supportsUnicodeHints() {
		return "→ use and refine"
	}
	return "Right: use and refine"
}

func tabSwitchHintLabel() string {
	if supportsUnicodeHints() {
		return "⇥ switch context"
	}
	return "Tab: switch context"
}

func supportsUnicodeHints() bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if value == "" {
			continue
		}
		return strings.Contains(value, "utf-8") || strings.Contains(value, "utf8")
	}
	// Default to unicode when locale is unspecified; modern terminals handle it.
	return true
}
