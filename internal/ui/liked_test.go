package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

// newLikedModel shows a 100 track list while track 1 plays.
func newLikedModel(t *testing.T) Model {
	t.Helper()
	m := New(api.New("http://127.0.0.1:1")) // refuses connections
	m.width, m.height, m.pane = 100, 30, paneTracks
	ctx := testCtx
	m = update(m, statusMsg{st: &api.Status{Username: "me", ContextURI: &ctx, Track: &api.Track{URI: trackURI(1), Name: "One"}}})
	ct := &api.ContextTracks{URI: testCtx, Ready: true, Length: 100, Cached: 100}
	for i := range 100 {
		ct.Tracks = append(ct.Tracks, api.ContextTrackItem{URI: trackURI(i), Track: &api.Track{URI: trackURI(i), Name: fmt.Sprint("T", i)}})
	}
	return update(m, listMsg{uri: testCtx, ct: ct})
}

func TestFetchLikedAsksForVisibleRowsOnce(t *testing.T) {
	m := newLikedModel(t)
	// The list load already asked for the playing track and visible rows.
	if !m.likedPending[trackURI(1)] || !m.likedPending[trackURI(m.offset)] {
		t.Fatalf("playing track and visible rows must be pending: %v", m.likedPending)
	}
	if m.likedPending[trackURI(99)] {
		t.Fatalf("rows below the view must not be asked for")
	}
	if cmd := m.fetchLiked(); cmd != nil {
		t.Fatalf("pending rows must not be asked for twice")
	}

	m = update(m, likedMsg{uris: []string{trackURI(1)}, states: map[string]bool{trackURI(1): true}})
	if liked, known := m.likedState(trackURI(1)); !liked || !known {
		t.Fatalf("track 1: liked=%v known=%v", liked, known)
	}
	if m.likedPending[trackURI(1)] {
		t.Fatalf("an answered uri must leave pending")
	}
}

func TestLikedUnavailableStopsAsking(t *testing.T) {
	m := newLikedModel(t)
	m = update(m, likedMsg{uris: []string{trackURI(1)}, err: api.ErrLikedUnavailable})
	m.likedPending = map[string]bool{}
	if cmd := m.fetchLiked(); cmd != nil {
		t.Fatalf("an old daemon must not be asked again")
	}
}

func TestToggleLiked(t *testing.T) {
	m := newLikedModel(t)
	m.cursor = 5

	_, cmd := m.toggleLiked()
	if msg := cmd().(libraryWriteMsg); msg.likedURI != trackURI(5) || !msg.liked {
		t.Fatalf("an unliked track must be added: %+v", msg)
	}

	m.liked[trackURI(5)] = true
	_, cmd = m.toggleLiked()
	if msg := cmd().(libraryWriteMsg); msg.liked || !strings.HasPrefix(msg.note, "Removed") {
		t.Fatalf("a liked track must be removed: %+v", msg)
	}
}

func TestLibraryWriteUpdatesLiked(t *testing.T) {
	m := newLikedModel(t)
	m = update(m, libraryWriteMsg{note: "x", likedURI: trackURI(7), liked: true})
	if liked, _ := m.likedState(trackURI(7)); !liked {
		t.Fatalf("a successful like must show at once")
	}
	m = update(m, libraryWriteMsg{note: "x", likedURI: trackURI(7), liked: false})
	if liked, known := m.likedState(trackURI(7)); liked || !known {
		t.Fatalf("a successful unlike must show at once")
	}
}

func TestHeartsAreRendered(t *testing.T) {
	m := newLikedModel(t)
	m = update(m, likedMsg{uris: []string{trackURI(1)}, states: map[string]bool{trackURI(1): true}})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "One ♥") {
		t.Fatalf("now playing must carry a heart:\n%s", view)
	}
	found := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "  2 T1 ") {
			found = true
			if !strings.Contains(line, "♥") {
				t.Fatalf("liked row must carry a heart: %q", line)
			}
		}
	}
	if !found {
		t.Fatalf("row of track 1 not rendered:\n%s", view)
	}
}
