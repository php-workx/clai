package picker

import (
	"context"
	"fmt"
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
	stateCancelled                    // User cancelled (Esc / Ctrl+C)
)

// fetchDoneMsg is sent when an async Provider.Fetch completes.
type fetchDoneMsg struct {
	requestID uint64
	items     []string
	atEnd     bool
	err       error
}

// debounceMsg fires after the debounce timer expires.
type debounceMsg struct {
	id uint64 // Must match current requestID to be accepted
}

// initMsg is sent by Init() to trigger the first fetch via Update(),
// ensuring state mutations are visible to the Bubble Tea runtime.
type initMsg struct{}

// Model is the Bubble Tea model for the history picker TUI.
// It must be exported so that cmd/clai-picker can use it.
type Model struct {
	state     pickerState
	tabs      []config.TabDef
	activeTab int
	items     []string
	selection int // Index into items; -1 when empty
	textInput textinput.Model
	offset    int  // Pagination offset
	atEnd     bool // No more pages from provider
	err       error

	requestID uint64 // Monotonic counter for stale detection
	provider  Provider

	width  int // Terminal width
	height int // Terminal height

	// result holds the selected command after the user presses Enter.
	result string

	// cancelFetch cancels the in-flight Provider.Fetch context.
	cancelFetch context.CancelFunc

	// debounceID tracks the latest debounce timer; only a matching
	// debounceMsg will trigger a fetch.
	debounceID uint64

	// layout controls the visual arrangement of list items.
	layout Layout
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
func (m Model) WithQuery(q string) Model {
	m.textInput.SetValue(q)
	return m
}

// WithLayout returns a copy of the Model with the given layout.
func (m Model) WithLayout(l Layout) Model {
	m.layout = l
	return m
}

// Result returns the selected command string, or "" if cancelled.
func (m Model) Result() string {
	return m.result
}

// Init implements tea.Model. It sends an initMsg so that the first fetch
// is triggered through Update, where state mutations are properly captured.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		func() tea.Msg { return initMsg{} },
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textInput.Width = msg.Width - 4 // account for prompt prefix
		return m, nil

	case fetchDoneMsg:
		return m.handleFetchDone(msg)

	case debounceMsg:
		return m.handleDebounce(msg)

	case initMsg:
		return m, m.startFetch()
	}

	// Forward to textinput for cursor blink and other internal messages.
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.state = stateCancelled
		m.cancelInflight()
		return m, tea.Quit

	case tea.KeyEnter:
		if m.selection >= 0 && m.selection < len(m.items) {
			m.result = m.items[m.selection]
		}
		m.cancelInflight()
		return m, tea.Quit

	case tea.KeyUp:
		if m.state == stateLoading {
			return m, nil
		}
		if m.layout == LayoutBottomUp {
			if m.selection < len(m.items)-1 {
				m.selection++
			}
		} else {
			if m.selection > 0 {
				m.selection--
			}
		}
		return m, nil

	case tea.KeyDown:
		if m.state == stateLoading {
			return m, nil
		}
		if m.layout == LayoutBottomUp {
			if m.selection > 0 {
				m.selection--
			}
		} else {
			if m.selection < len(m.items)-1 {
				m.selection++
			}
		}
		return m, nil

	case tea.KeyTab:
		if len(m.tabs) > 1 {
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			m.offset = 0
			return m, m.startFetch()
		}
		return m, nil
	}

	// Delegate all other keys (typing, backspace, etc.) to textinput.
	prevQuery := m.textInput.Value()
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)

	// If the query changed, trigger a debounced search.
	if m.textInput.Value() != prevQuery {
		m.offset = 0
		return m, tea.Batch(cmd, m.startDebounce())
	}
	return m, cmd
}

// handleFetchDone processes the result of an async fetch.
func (m Model) handleFetchDone(msg fetchDoneMsg) (tea.Model, tea.Cmd) {
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

	m.items = msg.items
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
func (m Model) handleDebounce(msg debounceMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.debounceID {
		return m, nil // Stale debounce timer; ignore.
	}
	return m, m.startFetch()
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
func (m Model) currentTab() config.TabDef {
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab]
	}
	return config.TabDef{ID: "default", Label: "Default"}
}

// listHeight returns the number of visible list rows (terminal height minus
// header and footer).
func (m Model) listHeight() int {
	// 1 row for tab bar, 1 row for query line, 1 row for newlines between sections.
	chrome := 3
	if m.layout == LayoutBottomUp {
		chrome++ // +1 for separator line between items and query
	}
	h := m.height - chrome
	if h < 1 {
		h = 20 // Sensible default before first WindowSizeMsg
	}
	return h
}

// separatorWidth returns the width of the horizontal separator line.
func (m Model) separatorWidth() int {
	w := m.width
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
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder

	// Tab bar
	b.WriteString(m.viewTabBar())
	b.WriteRune('\n')

	// Main content area
	b.WriteString(m.viewContent())
	b.WriteRune('\n')

	// Separator between items and query (BottomUp only)
	if m.layout == LayoutBottomUp {
		b.WriteString(dimStyle.Render(strings.Repeat("─", m.separatorWidth())))
		b.WriteRune('\n')
	}

	// Query line
	b.WriteString(m.viewQuery())

	return b.String()
}

// viewTabBar renders the tab bar.
func (m Model) viewTabBar() string {
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
	return strings.Join(parts, " ")
}

// viewContent renders the item list or a status message.
func (m Model) viewContent() string {
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
func (m Model) viewList() string {
	maxItems := m.listHeight()
	n := len(m.items)
	if n > maxItems {
		n = maxItems
	}

	// Build rendered lines for visible items.
	query := m.textInput.Value()
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		display := m.items[i]
		if m.width > 4 {
			display = MiddleTruncate(StripANSI(display), m.width-4)
		}
		if i == m.selection {
			lines = append(lines, selectedStyle.Render("> ")+highlightQuery(display, query, selectedStyle, matchSelectedStyle))
		} else {
			lines = append(lines, normalStyle.Render("  ")+highlightQuery(display, query, normalStyle, matchStyle))
		}
	}

	if m.layout == LayoutBottomUp {
		// Reverse so newest (index 0) is at bottom, closest to input.
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		// Pad above to bottom-align items.
		pad := maxItems - len(lines)
		if pad > 0 {
			padding := make([]string, pad)
			for i := range padding {
				padding[i] = ""
			}
			lines = append(padding, lines...)
		}
	}

	return strings.Join(lines, "\n")
}

// highlightQuery renders display text with occurrences of query highlighted.
// Matching is case-insensitive. Non-matching segments use base style;
// matching segments use highlight style.
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
func (m Model) viewQuery() string {
	return m.textInput.View()
}
