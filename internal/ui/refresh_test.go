package ui

import (
	"testing"

	"github.com/isnogudus/respot/internal/api"
)

func notReady(uri string) *api.ContextTracks {
	return &api.ContextTracks{URI: uri}
}

func TestAddedTrackShowsUpInTheOpenPlaylist(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "c") // Now playing lists testCtx
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 3)})

	nm, cmd := m.Update(libraryWriteMsg{note: "Added", changed: testCtx, playlistsChanged: true})
	m = nm.(Model)
	if cmd == nil || !m.lists[testCtx].stale || !m.library.playlistsState.loading {
		t.Fatalf("a write must reload the open listing and the playlists")
	}

	// The daemon enumerates the playlist again: the old rows stay meanwhile.
	m = update(m, listMsg{uri: testCtx, ct: notReady(testCtx)})
	if got := len(m.rows()); got != 3 {
		t.Fatalf("rows while re-enumerating = %d, want the old 3", got)
	}

	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 4)})
	if got := len(m.rows()); got != 4 || m.lists[testCtx].stale {
		t.Fatalf("rows = %d, stale = %v; want the new 4 and fresh", got, m.lists[testCtx].stale)
	}
	if c := m.syncList(); c != nil {
		t.Fatalf("a fresh complete listing must not load again")
	}
}

func TestChangedListingLoadsAgainWhenOpened(t *testing.T) {
	m := newTestModel(t)
	m = update(m, listMsg{uri: testAlbum, ct: testTracks(testAlbum, 2)}) // cached, not shown

	nm, cmd := m.Update(libraryWriteMsg{note: "Added", changed: testAlbum})
	m = nm.(Model)
	if cmd != nil {
		t.Fatalf("a listing no page shows is not loaded right away")
	}
	if !m.lists[testAlbum].stale {
		t.Fatalf("but it is marked as changed")
	}

	m = press(m, "down", "down", "down", "right") // Albums
	_, cmd = m.Update(key("right"))               // the album
	if cmd == nil {
		t.Fatalf("opening a changed listing must load it again")
	}
}

func TestLikingChangesLikedSongs(t *testing.T) {
	m := newTestModel(t)
	msg := m.setLikedCmd(trackRef{uri: trackURI(1), title: "T1"}, true)().(libraryWriteMsg)
	if msg.changed != likedSongsURI(testUsername) {
		t.Fatalf("changed = %q, want Liked Songs", msg.changed)
	}
}
