package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

func newPopupModel(t *testing.T) Model {
	t.Helper()
	m := New(api.New("http://invalid"))
	m.width, m.height = 100, 30
	ctx := testCtx
	m = update(m, statusMsg{st: &api.Status{
		Username:   "me",
		ContextURI: &ctx,
		Track:      &api.Track{URI: "spotify:track:playing", Name: "Playing", ArtistNames: []string{"Band"}},
	}})
	return update(m, libraryMsg{playlists: []api.LibraryPlaylist{
		{URI: "spotify:playlist:mine", Name: "Mine", CanEdit: true},
		{URI: "spotify:playlist:theirs", Name: "Theirs"},
	}})
}

func TestAddPopupTargetsPlayingTrack(t *testing.T) {
	m := update(newPopupModel(t), key("A"))
	if m.popup == nil || m.popup.uri != "spotify:track:playing" || m.popup.title != "Playing — Band" {
		t.Fatalf("popup = %+v, want the playing track", m.popup)
	}
}

func TestAddPopupTargetsSelectedTrack(t *testing.T) {
	m := newPopupModel(t)
	m.pane = paneTracks
	m.listURI = testCtx
	m.list = &api.ContextTracks{URI: testCtx, Ready: true, Length: 2, Cached: 2, Tracks: []api.ContextTrackItem{
		{URI: "spotify:track:first"}, {URI: "spotify:track:second"},
	}}
	m.cursor = 1
	m = update(m, key("A"))
	if m.popup == nil || m.popup.uri != "spotify:track:second" {
		t.Fatalf("popup = %+v, want the selected track", m.popup)
	}
}

func TestAddPopupOffersEditablePlaylistsOnly(t *testing.T) {
	m := newPopupModel(t)
	var names []string
	for _, e := range m.popupEntries() {
		names = append(names, e.name)
	}
	if strings.Join(names, ",") != likedSongsName+",Mine" {
		t.Fatalf("popup entries = %v, want Liked Songs and Mine", names)
	}
}

func TestAddPopupKeys(t *testing.T) {
	m := update(newPopupModel(t), key("A"))

	nm, cmd := m.Update(key("q"))
	m = nm.(Model)
	if m.popup != nil || cmd != nil {
		t.Fatalf("q must close the popup, not quit")
	}

	m = update(m, key("A"))
	m = update(m, key("j"))
	if m.popup.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.popup.cursor)
	}
	nm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.popup != nil || cmd == nil {
		t.Fatalf("enter must close the popup and start the add")
	}
}

func TestAddPopupKeepsKeyLine(t *testing.T) {
	m := update(newPopupModel(t), key("A"))
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("view has %d lines, want %d", len(lines), m.height)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Add to …") || !strings.Contains(view, "Mine") {
		t.Fatalf("popup not drawn:\n%s", view)
	}
	if last := ansi.Strip(lines[len(lines)-1]); !strings.Contains(last, "play/pause") {
		t.Fatalf("last line is %q, want the key line", last)
	}
}

func TestLibraryWriteResult(t *testing.T) {
	m := update(newPopupModel(t), libraryWriteMsg{note: "Added X to Mine"})
	if m.note != "Added X to Mine" || m.err != nil {
		t.Fatalf("note=%q err=%v", m.note, m.err)
	}
}
