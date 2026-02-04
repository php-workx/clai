package picker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/config"
)

// --- Mock provider ---

type mockProvider struct {
	items []string
	atEnd bool
	err   error
	delay time.Duration // Optional delay to simulate slow fetch
}

func (p *mockProvider) Fetch(ctx context.Context, req Request) (Response, error) {
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	}
	if p.err != nil {
		return Response{}, p.err
	}
	return Response{
		RequestID: req.RequestID,
		Items:     p.items,
		AtEnd:     p.atEnd,
	}, nil
}

func defaultTabs() []config.TabDef {
	return []config.TabDef{
		{ID: "session", Label: "Session", Provider: "history"},
		{ID: "global", Label: "Global", Provider: "history"},
	}
}

func newTestModel(p Provider) Model {
	m := NewModel(defaultTabs(), p)
	m.width = 80
	m.height = 24
	return m
}

// runCmd executes a tea.Cmd synchronously and returns the resulting message.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// drainBatch runs a batch cmd and feeds all resulting messages into the model,
// returning the final model state and any remaining cmd from the last message.
func drainBatch(t *testing.T, m Model, batchCmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	msg := runCmd(batchCmd)
	if msg == nil {
		return m, nil
	}
	// tea.Batch produces a tea.BatchMsg ([]tea.Cmd) when run.
	if batch, ok := msg.(tea.BatchMsg); ok {
		var lastCmd tea.Cmd
		for _, cmd := range batch {
			sub := runCmd(cmd)
			if sub == nil {
				continue
			}
			var result tea.Model
			result, lastCmd = m.Update(sub)
			m = result.(Model)
		}
		return m, lastCmd
	}
	// Single message.
	result, cmd := m.Update(msg)
	return result.(Model), cmd
}

// initAndLoad runs the full Init -> fetch cycle,
// returning the model in its post-fetch state (loaded, empty, or error).
func initAndLoad(t *testing.T, m Model) Model {
	t.Helper()

	// Init() returns a batch (textinput.Blink + initMsg).
	// Drain the batch to process all messages including initMsg.
	initCmd := m.Init()
	m, fetchCmd := drainBatch(t, m, initCmd)
	require.Equal(t, stateLoading, m.state)

	// Run fetchCmd -> produces fetchDoneMsg
	fetchDoneMsgVal := runCmd(fetchCmd)
	require.NotNil(t, fetchDoneMsgVal)

	// Process fetchDoneMsg -> transitions to loaded/empty/error
	result, _ := m.Update(fetchDoneMsgVal)
	m = result.(Model)
	return m
}

// initToLoading runs just the Init -> initMsg cycle, leaving the model in
// stateLoading with an outstanding fetch command.
func initToLoading(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	initCmd := m.Init()
	m, fetchCmd := drainBatch(t, m, initCmd)
	require.Equal(t, stateLoading, m.state)
	return m, fetchCmd
}

// --- State transition tests ---

func TestInitialState(t *testing.T) {
	p := &mockProvider{}
	m := newTestModel(p)
	assert.Equal(t, stateIdle, m.state)
	assert.Equal(t, -1, m.selection)
}

func TestInit_TransitionsToLoading(t *testing.T) {
	p := &mockProvider{items: []string{"ls", "cd"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	assert.Equal(t, stateLoaded, m.state)
	assert.Equal(t, []string{"ls", "cd"}, m.items)
	assert.True(t, m.atEnd)
}

func TestLoading_ToEmpty(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	assert.Equal(t, stateEmpty, m.state)
	assert.Equal(t, -1, m.selection)
}

func TestLoading_ToError(t *testing.T) {
	p := &mockProvider{err: errors.New("connection refused")}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	assert.Equal(t, stateError, m.state)
	assert.EqualError(t, m.err, "connection refused")
	assert.Equal(t, -1, m.selection)
}

func TestLoaded_ToLoading_OnTabChange(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateLoaded, m.state)

	// Press Tab to switch tabs
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	assert.Equal(t, 1, m.activeTab)
	assert.Equal(t, stateLoading, m.state)
}

func TestAnyCancelledOnEsc(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	// Press Esc
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)
	assert.Equal(t, stateCancelled, m.state)
	assert.Empty(t, m.Result())

	// Should return tea.Quit
	quitMsg := runCmd(cmd)
	assert.NotNil(t, quitMsg)
}

func TestCtrlC_CopiesSelectedItem(t *testing.T) {
	p := &mockProvider{items: []string{"ls -la", "pwd"}, atEnd: true}
	m := newTestModel(p)
	m = initAndLoad(t, m)
	assert.Equal(t, 0, m.selection)

	// Ctrl+C should return a command (clipboard copy), not quit.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = result.(Model)
	assert.NotEqual(t, stateCancelled, m.state)
	assert.NotNil(t, cmd, "Ctrl+C with selection should return a clipboard command")
}

func TestCtrlC_NoSelection_NoOp(t *testing.T) {
	p := &mockProvider{items: nil, atEnd: true}
	m := newTestModel(p)
	m = initAndLoad(t, m)
	assert.Equal(t, -1, m.selection)

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = result.(Model)
	assert.Nil(t, cmd)
	assert.NotEqual(t, stateCancelled, m.state)
}

func TestError_ToLoading_OnTabChange(t *testing.T) {
	p := &mockProvider{err: errors.New("fail")}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateError, m.state)

	// Fix the provider and press Tab
	p.err = nil
	p.items = []string{"ls"}
	p.atEnd = true

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	assert.Equal(t, stateLoading, m.state)

	msg := runCmd(cmd)
	result, _ = m.Update(msg)
	m = result.(Model)
	assert.Equal(t, stateLoaded, m.state)
}

// --- Selection bounds tests ---

func TestSelectionClamped_AfterItemsShrink(t *testing.T) {
	p := &mockProvider{items: []string{"a", "b", "c", "d", "e"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	m.selection = 4

	// New fetch returns fewer items
	p.items = []string{"a", "b"}
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	msg := runCmd(cmd)
	result, _ = m.Update(msg)
	m = result.(Model)

	assert.Equal(t, stateLoaded, m.state)
	assert.Equal(t, 1, m.selection) // Clamped to len-1
}

func TestSelectionClamped_EmptyItems(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, 0, m.selection)

	// Fetch returns empty
	p.items = []string{}
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	msg := runCmd(cmd)
	result, _ = m.Update(msg)
	m = result.(Model)

	assert.Equal(t, stateEmpty, m.state)
	assert.Equal(t, -1, m.selection)
}

func TestSelectionClamped_NegativeToZero(t *testing.T) {
	p := &mockProvider{items: []string{"a", "b"}, atEnd: true}
	m := newTestModel(p)
	m.selection = -1 // Starts at -1

	m = initAndLoad(t, m)

	assert.Equal(t, 0, m.selection) // Clamped from -1 to 0
}

// --- Stale response tests ---

func TestStaleResponse_Discarded(t *testing.T) {
	p := &mockProvider{items: []string{"first"}, atEnd: true}
	m := newTestModel(p)

	m, _ = initToLoading(t, m)
	currentID := m.requestID

	// Simulate a stale response from an earlier request
	staleMsg := fetchDoneMsg{
		requestID: currentID - 1,
		items:     []string{"stale"},
	}
	result, _ := m.Update(staleMsg)
	m = result.(Model)

	assert.Equal(t, stateLoading, m.state)
	assert.Empty(t, m.items)
}

func TestCurrentResponse_Accepted(t *testing.T) {
	p := &mockProvider{items: []string{"current"}, atEnd: true}
	m := newTestModel(p)

	m, fetchCmd := initToLoading(t, m)
	currentID := m.requestID

	msg := runCmd(fetchCmd)
	doneMsg := msg.(fetchDoneMsg)
	assert.Equal(t, currentID, doneMsg.requestID)

	result, _ := m.Update(msg)
	m = result.(Model)
	assert.Equal(t, stateLoaded, m.state)
	assert.Equal(t, []string{"current"}, m.items)
}

// --- Key handling tests ---

func TestUpDown_Navigation(t *testing.T) {
	p := &mockProvider{items: []string{"a", "b", "c"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, 0, m.selection)

	// Down
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 1, m.selection)

	// Down again
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 2, m.selection)

	// Down at bottom - stays
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 2, m.selection)

	// Up
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, 1, m.selection)

	// Up
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, 0, m.selection)

	// Up at top - stays
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, 0, m.selection)
}

func TestUpDown_NoOp_DuringLoading(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	m, _ = initToLoading(t, m)
	m.selection = 0

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 0, m.selection) // Unchanged
}

func TestEnter_SelectsItem(t *testing.T) {
	p := &mockProvider{items: []string{"ls -la", "pwd"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	// Move to second item
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 1, m.selection)

	// Enter
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)
	assert.Equal(t, "pwd", m.Result())
	assert.NotNil(t, cmd) // tea.Quit
}

func TestEnter_EmptyList_NoResult(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)
	assert.Empty(t, m.Result())
}

func TestTabCycling(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, 0, m.activeTab)

	// Tab
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	assert.Equal(t, 1, m.activeTab)

	// Tab again - wraps
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	assert.Equal(t, 0, m.activeTab)
}

func TestTabResetsOffset(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m.offset = 50

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	assert.Equal(t, 0, m.offset)
}

// --- Query / debounce tests ---

func TestTyping_AppendsToQuery(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = result.(Model)
	assert.Equal(t, "l", m.textInput.Value())

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = result.(Model)
	assert.Equal(t, "ls", m.textInput.Value())
}

func TestBackspace_RemovesFromQuery(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m.textInput.SetValue("ls")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = result.(Model)
	assert.Equal(t, "l", m.textInput.Value())
}

func TestBackspace_EmptyQuery_NoOp(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m.textInput.SetValue("")

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = result.(Model)
	assert.Equal(t, "", m.textInput.Value())
	// textinput may return a blink cmd even on empty backspace
	_ = cmd
}

func TestDebounce_NewKeystrokeCancelsPrevious(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	// Type 'l' - starts debounce with debounceID 1
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = result.(Model)
	firstDebounceID := m.debounceID

	// Type 's' - starts new debounce with debounceID 2
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = result.(Model)
	secondDebounceID := m.debounceID

	assert.Greater(t, secondDebounceID, firstDebounceID)

	// Old debounce fires - should be ignored
	result, cmd := m.Update(debounceMsg{id: firstDebounceID})
	m = result.(Model)
	assert.Nil(t, cmd)
}

func TestDebounce_CurrentTimerTriggersFetch(t *testing.T) {
	p := &mockProvider{items: []string{"found"}, atEnd: true}
	m := newTestModel(p)

	// Type 'l' - starts debounce
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = result.(Model)
	currentDebounceID := m.debounceID

	// Current debounce fires
	result, cmd := m.Update(debounceMsg{id: currentDebounceID})
	m = result.(Model)
	require.NotNil(t, cmd)
	assert.Equal(t, stateLoading, m.state)
}

// --- WindowSizeMsg ---

func TestWindowResize(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = result.(Model)
	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
}

func TestWindowResize_PreservesSelection(t *testing.T) {
	p := &mockProvider{items: []string{"a", "b", "c"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	m.selection = 2

	result, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = result.(Model)
	assert.Equal(t, 2, m.selection) // Preserved
}

// --- View rendering ---

func TestView_ShowsTabBar(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)

	view := m.View()
	assert.Contains(t, view, "Session")
	assert.Contains(t, view, "Global")
}

func TestView_ShowsQueryLine(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m.textInput.SetValue("test")

	view := m.View()
	assert.Contains(t, view, "test")
}

func TestView_ShowsLoadingState(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m.state = stateLoading

	view := m.View()
	assert.Contains(t, view, "Loading...")
}

func TestView_ShowsEmptyState(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)
	m.state = stateEmpty

	view := m.View()
	assert.Contains(t, view, "No matches")
}

func TestView_ShowsErrorState(t *testing.T) {
	p := &mockProvider{items: nil, atEnd: true}
	m := newTestModel(p)
	m.state = stateError
	m.err = errors.New("test error")

	view := m.View()
	assert.Contains(t, view, "test error")
}

func TestResult_EmptyOnCancel(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)
	assert.Empty(t, m.Result())
}

// --- Single tab: Tab key is no-op ---

func TestSingleTab_TabIsNoOp(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	tabs := []config.TabDef{{ID: "session", Label: "Session"}}
	m := NewModel(tabs, p)
	m.width = 80
	m.height = 24

	m = initAndLoad(t, m)

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	assert.Equal(t, 0, m.activeTab)
	assert.Nil(t, cmd) // No fetch triggered
}

// --- Empty list: Up/Down are no-ops ---

func TestUpDown_NoOp_WhenEmpty(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateEmpty, m.state)
	assert.Equal(t, -1, m.selection)

	// Down should be a no-op.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, -1, m.selection)

	// Up should be a no-op.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, -1, m.selection)
}

func TestEsc_WorksWhenEmpty(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateEmpty, m.state)

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)
	assert.Equal(t, stateCancelled, m.state)
	assert.Empty(t, m.Result())
	assert.NotNil(t, cmd)
}

// --- Zero search results: Enter is no-op, query still editable ---

func TestZeroResults_EnterIsNoOp(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateEmpty, m.state)

	// Enter should produce no result.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)
	assert.Empty(t, m.Result())
}

func TestZeroResults_QueryEditable(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateEmpty, m.state)

	// Should still be able to type.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = result.(Model)
	assert.Equal(t, "x", m.textInput.Value())
	assert.NotNil(t, cmd) // Debounce timer started
}

// --- Selection bounds: items grow ---

func TestSelectionClamped_ItemsGrow(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, 0, m.selection)

	// Tab to switch to a result with more items.
	p.items = []string{"a", "b", "c", "d", "e"}
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	msg := runCmd(cmd)
	result, _ = m.Update(msg)
	m = result.(Model)

	assert.Equal(t, stateLoaded, m.state)
	// Selection should remain at 0 (still valid).
	assert.Equal(t, 0, m.selection)
}

// --- Query change triggers loading (via debounce -> fetch) ---

func TestQueryChange_TriggersLoadingViaDebounce(t *testing.T) {
	p := &mockProvider{items: []string{"abc"}, atEnd: true}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateLoaded, m.state)

	// Type a character - starts debounce.
	result, debounceCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = result.(Model)
	debounceID := m.debounceID

	// Fire the debounce.
	result, fetchCmd := m.Update(debounceMsg{id: debounceID})
	m = result.(Model)
	require.NotNil(t, fetchCmd)
	assert.Equal(t, stateLoading, m.state)

	// Complete the fetch.
	p.items = []string{"abc"}
	fetchResult := runCmd(fetchCmd)
	result, _ = m.Update(fetchResult)
	m = result.(Model)
	assert.Equal(t, stateLoaded, m.state)

	// Suppress unused warning.
	_ = debounceCmd
}

// --- Error state: query still editable, retry via query change ---

func TestError_QueryEditableAndRetryViaDebounce(t *testing.T) {
	p := &mockProvider{err: errors.New("fail")}
	m := newTestModel(p)

	m = initAndLoad(t, m)
	assert.Equal(t, stateError, m.state)

	// Fix the provider.
	p.err = nil
	p.items = []string{"recovered"}
	p.atEnd = true

	// Type to start a new search.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = result.(Model)
	assert.Equal(t, "r", m.textInput.Value())
	debounceID := m.debounceID

	// Fire the debounce -> starts fetch.
	result, fetchCmd := m.Update(debounceMsg{id: debounceID})
	m = result.(Model)
	assert.Equal(t, stateLoading, m.state)

	// Complete the fetch.
	fetchResult := runCmd(fetchCmd)
	result, _ = m.Update(fetchResult)
	m = result.(Model)
	assert.Equal(t, stateLoaded, m.state)
	assert.Equal(t, []string{"recovered"}, m.items)
}

// --- Cancelled state: View shows "Cancelled" ---

func TestView_ShowsCancelledState(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m.state = stateCancelled

	view := m.View()
	assert.Contains(t, view, "Cancelled")
}

// --- WithQuery sets initial query ---

func TestWithQuery(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	m = m.WithQuery("initial")
	assert.Equal(t, "initial", m.textInput.Value())
}

// --- Init returns a cmd ---

func TestInit_ReturnsCmd(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p)
	cmd := m.Init()
	assert.NotNil(t, cmd)
}

// --- Loading state: Enter is no-op ---

func TestEnter_NoOp_DuringLoading(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true, delay: 1 * time.Second}
	m := newTestModel(p)

	m, _ = initToLoading(t, m)
	assert.Equal(t, stateLoading, m.state)

	// Enter during loading should quit but produce no result.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)
	assert.Empty(t, m.Result())
	assert.NotNil(t, cmd) // tea.Quit
}

// --- Layout tests ---

func newBottomUpModel(p Provider) Model {
	m := newTestModel(p)
	m.layout = LayoutBottomUp
	return m
}

func TestBottomUp_ViewList_ReversesItems(t *testing.T) {
	p := &mockProvider{items: []string{"newest", "middle", "oldest"}, atEnd: true}
	m := newBottomUpModel(p)
	m = initAndLoad(t, m)

	view := m.viewList()
	lines := strings.Split(view, "\n")

	// Find non-empty lines (skip padding).
	var items []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}

	require.Len(t, items, 3)
	// Reversed: oldest at top, newest at bottom (closest to input).
	assert.Contains(t, items[0], "oldest")
	assert.Contains(t, items[1], "middle")
	assert.Contains(t, items[2], "newest")
}

func TestBottomUp_ViewList_BottomAligned(t *testing.T) {
	p := &mockProvider{items: []string{"a", "b"}, atEnd: true}
	m := newBottomUpModel(p)
	m = initAndLoad(t, m)

	view := m.viewList()
	lines := strings.Split(view, "\n")
	maxItems := m.listHeight()

	// Total lines should equal listHeight (items + padding).
	assert.Equal(t, maxItems, len(lines))

	// First lines should be empty (padding).
	padCount := maxItems - 2
	for i := 0; i < padCount; i++ {
		assert.Empty(t, lines[i], "line %d should be padding", i)
	}
}

func TestBottomUp_View_HasSeparator(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newBottomUpModel(p)
	m = initAndLoad(t, m)

	view := m.View()
	assert.Contains(t, view, "──")
}

func TestBottomUp_UpDown_InvertedNavigation(t *testing.T) {
	p := &mockProvider{items: []string{"newest", "middle", "oldest"}, atEnd: true}
	m := newBottomUpModel(p)
	m = initAndLoad(t, m)
	assert.Equal(t, 0, m.selection) // Starts at newest

	// Up should move to older (higher index = visually higher).
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, 1, m.selection)

	// Up again.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, 2, m.selection)

	// Up at top - stays.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(Model)
	assert.Equal(t, 2, m.selection)

	// Down moves toward newer (lower index = visually lower).
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 1, m.selection)

	// Down again.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 0, m.selection)

	// Down at bottom - stays.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(Model)
	assert.Equal(t, 0, m.selection)
}

func TestBottomUp_SelectionMarkerOnCorrectItem(t *testing.T) {
	p := &mockProvider{items: []string{"newest", "oldest"}, atEnd: true}
	m := newBottomUpModel(p)
	m = initAndLoad(t, m)
	// Selection 0 = "newest", which should be at the bottom visually.

	view := m.viewList()
	lines := strings.Split(view, "\n")

	// Find the line with the selection marker.
	var selectedLine string
	for _, l := range lines {
		if strings.Contains(l, ">") {
			selectedLine = l
			break
		}
	}
	assert.Contains(t, selectedLine, "newest")
}

func TestTopDown_ViewList_PreservesOrder(t *testing.T) {
	p := &mockProvider{items: []string{"a", "b", "c"}, atEnd: true}
	m := newTestModel(p) // Default LayoutTopDown
	m = initAndLoad(t, m)

	view := m.viewList()
	lines := strings.Split(view, "\n")

	// Items should appear in original order, no padding.
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "a")
	assert.Contains(t, lines[1], "b")
	assert.Contains(t, lines[2], "c")
}

func TestTopDown_View_NoSeparator(t *testing.T) {
	p := &mockProvider{items: []string{"a"}, atEnd: true}
	m := newTestModel(p) // Default LayoutTopDown
	m = initAndLoad(t, m)

	view := m.View()
	assert.NotContains(t, view, "──")
}

func TestWithLayout(t *testing.T) {
	p := &mockProvider{}
	m := NewModel(defaultTabs(), p)
	assert.Equal(t, LayoutTopDown, m.layout)

	m = m.WithLayout(LayoutBottomUp)
	assert.Equal(t, LayoutBottomUp, m.layout)
}

func TestBottomUp_StatusMessages_BottomAligned(t *testing.T) {
	p := &mockProvider{items: []string{}, atEnd: true}
	m := newBottomUpModel(p)
	m = initAndLoad(t, m)
	assert.Equal(t, stateEmpty, m.state)

	content := m.viewContent()
	lines := strings.Split(content, "\n")
	// Should have padding lines before the status message.
	assert.Greater(t, len(lines), 1)
	// Last line should contain the status.
	assert.Contains(t, lines[len(lines)-1], "No matches")
}

func TestBottomUp_ListHeight_AccountsForSeparator(t *testing.T) {
	p := &mockProvider{}
	m := newTestModel(p)
	topDownHeight := m.listHeight()

	m.layout = LayoutBottomUp
	bottomUpHeight := m.listHeight()

	// BottomUp has 1 extra chrome row for separator.
	assert.Equal(t, topDownHeight-1, bottomUpHeight)
}

// --- highlightQuery tests ---

func TestHighlightQuery_EmptyQuery(t *testing.T) {
	result := highlightQuery("foobar", "", normalStyle, matchStyle)
	expected := normalStyle.Render("foobar")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_NoMatch(t *testing.T) {
	result := highlightQuery("foobar", "xyz", normalStyle, matchStyle)
	expected := normalStyle.Render("foobar")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_BasicMatch(t *testing.T) {
	result := highlightQuery("foobar", "foo", normalStyle, matchStyle)
	expected := matchStyle.Render("foo") + normalStyle.Render("bar")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_MatchAtEnd(t *testing.T) {
	result := highlightQuery("foobar", "bar", normalStyle, matchStyle)
	expected := normalStyle.Render("foo") + matchStyle.Render("bar")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_MatchInMiddle(t *testing.T) {
	result := highlightQuery("abcdef", "cd", normalStyle, matchStyle)
	expected := normalStyle.Render("ab") + matchStyle.Render("cd") + normalStyle.Render("ef")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_CaseInsensitive(t *testing.T) {
	result := highlightQuery("FooBar", "foo", normalStyle, matchStyle)
	// The original case is preserved in the highlight.
	expected := matchStyle.Render("Foo") + normalStyle.Render("Bar")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_MultipleMatches(t *testing.T) {
	result := highlightQuery("abXabXab", "ab", normalStyle, matchStyle)
	expected := matchStyle.Render("ab") + normalStyle.Render("X") +
		matchStyle.Render("ab") + normalStyle.Render("X") +
		matchStyle.Render("ab")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_EntireString(t *testing.T) {
	result := highlightQuery("foo", "foo", normalStyle, matchStyle)
	expected := matchStyle.Render("foo")
	assert.Equal(t, expected, result)
}

func TestHighlightQuery_SelectedStyle(t *testing.T) {
	result := highlightQuery("foobar", "foo", selectedStyle, matchSelectedStyle)
	expected := matchSelectedStyle.Render("foo") + selectedStyle.Render("bar")
	assert.Equal(t, expected, result)
}

func TestViewList_HighlightsQuery(t *testing.T) {
	p := &mockProvider{items: []string{"make build", "make test", "go build"}, atEnd: true}
	m := newTestModel(p)
	m = initAndLoad(t, m)

	// Set a query to trigger highlighting.
	m.textInput.SetValue("build")

	view := m.viewList()

	// The matched "build" portions should be rendered with matchStyle (yellow).
	// The non-matched portions should be rendered with normalStyle/selectedStyle.
	yellowRendered := matchStyle.Render("build")
	yellowSelectedRendered := matchSelectedStyle.Render("build")

	// "make build" is selected (index 0), so its "build" gets matchSelectedStyle.
	assert.Contains(t, view, yellowSelectedRendered)

	// "go build" is not selected, so its "build" gets matchStyle.
	assert.Contains(t, view, yellowRendered)

	// "make test" has no match — should NOT contain yellow "build".
	lines := strings.Split(view, "\n")
	// Find the "make test" line (index 1, not selected).
	for _, line := range lines {
		if strings.Contains(line, "make test") {
			assert.NotContains(t, line, yellowRendered)
			assert.NotContains(t, line, yellowSelectedRendered)
		}
	}
}

// --- Truncation styling tests ---

func TestViewList_TruncationStyled(t *testing.T) {
	// Use a very long item that will be truncated.
	longItem := strings.Repeat("abcdef", 20) // 120 chars
	p := &mockProvider{items: []string{longItem}, atEnd: true}
	m := newTestModel(p)
	m.width = 40 // Force truncation
	m = initAndLoad(t, m)

	view := m.viewList()
	// The truncation ellipsis should be styled with truncStyle (orange 208).
	styledEllipsis := truncStyle.Render(" \u2026 ")
	assert.Contains(t, view, styledEllipsis)
}

func TestRenderItem_NoTruncation(t *testing.T) {
	result := renderItem("short", "sh", normalStyle, matchStyle)
	// Should contain highlighted "sh" but no truncation styling.
	assert.Contains(t, result, matchStyle.Render("sh"))
	assert.NotContains(t, result, truncStyle.Render(" \u2026 "))
}

func TestRenderItem_WithTruncation(t *testing.T) {
	// Simulate a truncated string: "abcdef…xyz"
	display := "abcdef\u2026xyz"
	result := renderItem(display, "", normalStyle, matchStyle)
	assert.Contains(t, result, truncStyle.Render(" \u2026 "))
	assert.Contains(t, result, normalStyle.Render("abcdef"))
	assert.Contains(t, result, normalStyle.Render("xyz"))
}

// --- Clipboard + copied indicator tests ---

func TestClipboardMsg_SetsCopied(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)
	m = initAndLoad(t, m)

	result, cmd := m.Update(clipboardMsg{err: nil})
	m = result.(Model)
	assert.True(t, m.copied)
	assert.NotNil(t, cmd, "should return a tick command to clear the indicator")
}

func TestClipboardMsg_Error_NoCopied(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)
	m = initAndLoad(t, m)

	result, cmd := m.Update(clipboardMsg{err: fmt.Errorf("no clipboard")})
	m = result.(Model)
	assert.False(t, m.copied)
	assert.Nil(t, cmd)
}

func TestCopiedClearMsg_ClearsCopied(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)
	m.copied = true

	result, _ := m.Update(copiedClearMsg{})
	m = result.(Model)
	assert.False(t, m.copied)
}

func TestViewQuery_ShowsCopiedIndicator(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)
	m.copied = true

	view := m.viewQuery()
	assert.Contains(t, view, "Copied!")
}

func TestViewQuery_NoCopiedIndicator(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)
	m.copied = false

	view := m.viewQuery()
	assert.NotContains(t, view, "Copied!")
}

func TestEsc_StillCancels(t *testing.T) {
	p := &mockProvider{items: []string{"ls"}, atEnd: true}
	m := newTestModel(p)
	m = initAndLoad(t, m)

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)
	assert.Equal(t, stateCancelled, m.state)
	assert.NotNil(t, cmd, "Esc should return tea.Quit")
}
