package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const testCtx = "spotify:playlist:test"

func trackURI(i int) string { return fmt.Sprintf("spotify:track:%d", i) }

func statusPlaying(i int) statusMsg {
	ctx := testCtx
	return statusMsg{st: &api.Status{ContextURI: &ctx, Track: &api.Track{URI: trackURI(i)}}}
}

func newListModel(t *testing.T, playing int) Model {
	t.Helper()
	m := New(api.New("http://invalid"))
	m.width, m.height, m.pane = 100, 30, paneTracks
	m = update(m, statusPlaying(playing))
	ct := &api.ContextTracks{URI: testCtx, Ready: true, Length: 100, Cached: 100}
	for i := range 100 {
		ct.Tracks = append(ct.Tracks, api.ContextTrackItem{URI: trackURI(i)})
	}
	return update(m, listMsg{uri: testCtx, ct: ct})
}

func update(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestCursorFollowsPlayingTrack(t *testing.T) {
	m := newListModel(t, 10)
	if m.cursor != 10 {
		t.Fatalf("initial cursor = %d, want 10", m.cursor)
	}
	m = update(m, statusPlaying(80))
	if m.cursor != 80 {
		t.Fatalf("cursor after track change = %d, want 80", m.cursor)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+m.listHeight() {
		t.Fatalf("cursor %d not visible (offset %d, rows %d)", m.cursor, m.offset, m.listHeight())
	}
}

func TestCursorStaysWhenUserBrowses(t *testing.T) {
	m := newListModel(t, 10)
	m = update(m, key("j"))
	m = update(m, key("j"))
	m = update(m, statusPlaying(50))
	if m.cursor != 12 {
		t.Fatalf("cursor = %d, want 12 (user position kept)", m.cursor)
	}
	m = update(m, key("c"))
	if m.cursor != 50 {
		t.Fatalf("cursor after c = %d, want 50", m.cursor)
	}
	m = update(m, statusPlaying(51))
	if m.cursor != 51 {
		t.Fatalf("cursor after c + track change = %d, want 51", m.cursor)
	}
}

func TestCursorFollowsAfterEnter(t *testing.T) {
	m := newListModel(t, 10)
	for range 5 {
		m = update(m, key("j"))
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(m, statusPlaying(15))
	m = update(m, statusPlaying(16))
	if m.cursor != 16 {
		t.Fatalf("cursor = %d, want 16", m.cursor)
	}
}

func TestListStopsPollingWithoutProgress(t *testing.T) {
	m := newListModel(t, 10)
	partial := &api.ContextTracks{URI: testCtx, Ready: true, Length: 100, Cached: 90, Tracks: m.list.Tracks}

	var cmd tea.Cmd
	for i := 0; i < listMaxStalls; i++ {
		var nm tea.Model
		nm, cmd = m.Update(listMsg{uri: testCtx, ct: partial})
		m = nm.(Model)
	}
	if cmd != nil {
		t.Fatalf("still polling after %d polls without progress", listMaxStalls)
	}

	progressed := *partial
	progressed.Cached = 95
	if _, cmd = m.Update(listMsg{uri: testCtx, ct: &progressed}); cmd == nil {
		t.Fatalf("progress must resume polling")
	}
}
