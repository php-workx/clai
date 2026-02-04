package picker

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/runger/clai/internal/config"
)

// debounceInterval is the delay after the last keystroke before triggering a fetch.
const debounceInterval = 100 * time.Millisecond

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
	selection int    // Index into items; -1 when empty
	query     string // Current search query
	offset    int    // Pagination offset
	atEnd     bool   // No more pages from provider
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
}

// NewModel creates a new picker Model.
func NewModel(tabs []config.TabDef, provider Provider) Model {
	return Model{
		state:     stateIdle,
		tabs:      tabs,
		activeTab: 0,
		selection: -1,
		provider:  provider,
	}
}

// Result returns the selected command string, or "" if cancelled.
func (m Model) Result() string {
	return m.result
}

// Init implements tea.Model. It sends an initMsg so that the first fetch
// is triggered through Update, where state mutations are properly captured.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return initMsg{} }
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case fetchDoneMsg:
		return m.handleFetchDone(msg)

	case debounceMsg:
		return m.handleDebounce(msg)

	case initMsg:
		return m, m.startFetch()
	}

	return m, nil
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
		if m.selection > 0 {
			m.selection--
		}
		return m, nil

	case tea.KeyDown:
		if m.state == stateLoading {
			return m, nil
		}
		if m.selection < len(m.items)-1 {
			m.selection++
		}
		return m, nil

	case tea.KeyTab:
		if len(m.tabs) > 1 {
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			m.offset = 0
			return m, m.startFetch()
		}
		return m, nil

	case tea.KeyBackspace:
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.offset = 0
			return m, m.startDebounce()
		}
		return m, nil

	case tea.KeyRunes:
		m.query += string(msg.Runes)
		m.offset = 0
		return m, m.startDebounce()
	}

	return m, nil
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
		Query:     m.query,
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
	// 1 row for tab bar, 1 row for query line, 1 row for status
	const chrome = 3
	h := m.height - chrome
	if h < 1 {
		h = 20 // Sensible default before first WindowSizeMsg
	}
	return h
}

// --- View rendering ---

var (
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	inactiveTabStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	normalStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	queryStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
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

	// Query line
	b.WriteString(m.viewQuery())

	return b.String()
}

// viewTabBar renders the tab bar.
func (m Model) viewTabBar() string {
	var parts []string
	for i, tab := range m.tabs {
		label := " " + tab.Label + " "
		if i == m.activeTab {
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

// viewContent renders the item list or a status message.
func (m Model) viewContent() string {
	switch m.state {
	case stateIdle, stateLoading:
		return dimStyle.Render("Loading...")

	case stateEmpty:
		return dimStyle.Render("No matches")

	case stateError:
		msg := "Error"
		if m.err != nil {
			msg = fmt.Sprintf("Error: %s", m.err)
		}
		return errorStyle.Render(msg)

	case stateCancelled:
		return dimStyle.Render("Cancelled")

	case stateLoaded:
		return m.viewList()

	default:
		return ""
	}
}

// viewList renders the item list with selection marker.
func (m Model) viewList() string {
	var b strings.Builder
	maxItems := m.listHeight()
	for i, item := range m.items {
		if i >= maxItems {
			break
		}
		// Truncate long items to terminal width (minus marker prefix).
		display := item
		if m.width > 4 {
			display = MiddleTruncate(StripANSI(display), m.width-4)
		}

		if i == m.selection {
			b.WriteString(selectedStyle.Render("> " + display))
		} else {
			b.WriteString(normalStyle.Render("  " + display))
		}
		if i < len(m.items)-1 && i < maxItems-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

// viewQuery renders the query input line.
func (m Model) viewQuery() string {
	return queryStyle.Render("> ") + m.query
}
