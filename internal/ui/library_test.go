package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

func newLibraryModel(t *testing.T) Model {
	t.Helper()
	m := New(api.New("http://invalid"))
	m.width, m.height = 100, 30
	ctx := testCtx
	m = update(m, statusMsg{st: &api.Status{Username: "me", ContextURI: &ctx, Track: &api.Track{URI: trackURI(1)}}})
	nm, _ := m.openLibrary()
	m = nm.(Model)
	return update(m, libraryMsg{playlists: []api.LibraryPlaylist{
		{URI: "spotify:playlist:a", Name: "A", Length: 3},
		{URI: testCtx, Name: "Test", Length: 100, Folder: []string{"Folder"}},
	}})
}

func TestLibraryEntriesStartWithLikedSongs(t *testing.T) {
	m := newLibraryModel(t)
	entries := m.libraryEntries()
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if !entries[0].liked || entries[0].uri != "spotify:user:me:collection" {
		t.Fatalf("first entry = %+v, want Liked Songs", entries[0])
	}
	if entries[2].folder[0] != "Folder" {
		t.Fatalf("folder not kept: %+v", entries[2])
	}
}

func TestLibraryBrowseAndBack(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("j")) // playlist A
	m = update(m, key("l"))
	if m.pane != paneTracks || m.browseURI != "spotify:playlist:a" || m.listURI != "spotify:playlist:a" {
		t.Fatalf("after l: pane=%d browse=%q list=%q", m.pane, m.browseURI, m.listURI)
	}

	// A status update must not switch the list back to the playing context.
	ctx := testCtx
	m = update(m, statusMsg{st: &api.Status{Username: "me", ContextURI: &ctx, Track: &api.Track{URI: trackURI(2)}}})
	if m.listURI != "spotify:playlist:a" {
		t.Fatalf("list switched to %q while browsing", m.listURI)
	}

	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.pane != paneLibrary || m.browseURI != "" || m.libCursor != 1 {
		t.Fatalf("after esc: pane=%d browse=%q cursor=%d", m.pane, m.browseURI, m.libCursor)
	}
}

func TestBrowsedListDoesNotFollowPlayback(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("j"))
	m = update(m, key("l"))
	ct := &api.ContextTracks{URI: "spotify:playlist:a", Ready: true, Length: 3, Cached: 3}
	for i := range 3 {
		ct.Tracks = append(ct.Tracks, api.ContextTrackItem{URI: trackURI(i)})
	}
	m = update(m, listMsg{uri: "spotify:playlist:a", ct: ct})
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0: a browsed playlist must not follow the playing track", m.cursor)
	}
}

func TestLibraryKeyTogglesPane(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("b"))
	if m.pane != paneNone {
		t.Fatalf("pane = %d after second b, want none", m.pane)
	}
	nm, cmd := m.handleKey(key("b"))
	m = nm.(Model)
	if m.pane != paneLibrary || cmd != nil {
		t.Fatalf("reopening a loaded library must not refetch (pane=%d, cmd=%v)", m.pane, cmd != nil)
	}
}

func TestLibraryEnterOpensPlaylist(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("j"))
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.pane != paneTracks || m.browseURI != "spotify:playlist:a" {
		t.Fatalf("enter: pane=%d browse=%q, want the track list of playlist A", m.pane, m.browseURI)
	}
	if m.note != "" {
		t.Fatalf("enter must not start playback, got note %q", m.note)
	}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = update(m, key(string(r)))
	}
	return m
}

func TestLibraryFilter(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("j"))
	m = update(m, key("/"))
	m = typeText(m, "fol te") // folder "Folder" and name "Test"
	if got := m.libraryEntries(); len(got) != 1 || got[0].name != "Test" {
		t.Fatalf("filtered entries = %+v, want only Test", got)
	}
	if m.libCursor != 0 {
		t.Fatalf("cursor = %d, want reset to 0 while filtering", m.libCursor)
	}

	m = update(m, tea.KeyMsg{Type: tea.KeyEnter}) // keep filter, back to list
	if m.mode != inputNone || m.libFilter != "fol te" {
		t.Fatalf("after enter: mode=%d filter=%q", m.mode, m.libFilter)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter}) // open the match
	if m.browseURI != testCtx {
		t.Fatalf("enter opened %q, want the filtered match", m.browseURI)
	}

	m = update(m, tea.KeyMsg{Type: tea.KeyEsc}) // back to the library, filter kept
	if m.pane != paneLibrary || m.libFilter != "fol te" {
		t.Fatalf("after esc: pane=%d filter=%q", m.pane, m.libFilter)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc}) // clears the filter
	if m.pane != paneLibrary || m.libFilter != "" {
		t.Fatalf("first esc in library: pane=%d filter=%q, want filter cleared", m.pane, m.libFilter)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc}) // closes the library
	if m.pane != paneNone {
		t.Fatalf("second esc: pane=%d, want closed", m.pane)
	}
}

func TestLibraryFilterEscDropsFilter(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("/"))
	m = typeText(m, "zzz")
	if len(m.libraryEntries()) != 0 {
		t.Fatalf("expected no matches for zzz")
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.libFilter != "" || m.mode != inputNone || len(m.libraryEntries()) != 3 {
		t.Fatalf("esc while typing must drop the filter (filter=%q mode=%d)", m.libFilter, m.mode)
	}
}

func TestLibraryFilterIgnoresGlobalKeys(t *testing.T) {
	m := newLibraryModel(t)
	m = update(m, key("/"))
	m = typeText(m, "q")
	if m.libFilter != "q" {
		t.Fatalf("typing q in the filter must filter, not quit (filter=%q)", m.libFilter)
	}
}

func TestContextNameFallsBackToLibrary(t *testing.T) {
	m := newLibraryModel(t)
	liked := likedSongsURI("me")
	m = update(m, statusMsg{st: &api.Status{Username: "me", ContextURI: &liked}})
	if got := m.contextName(); got != likedSongsName {
		t.Fatalf("context name = %q, want %q for Liked Songs without a daemon name", got, likedSongsName)
	}

	named, name := testCtx, "From Daemon"
	m = update(m, statusMsg{st: &api.Status{Username: "me", ContextURI: &named, ContextName: &name}})
	if got := m.contextName(); got != name {
		t.Fatalf("context name = %q, want the daemon's %q", got, name)
	}
}
