package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/isnogudus/respot/internal/api"
)

// tracksOf lists the tracks with the given numbers, metadata included.
func tracksOf(uri string, numbers ...int) *api.ContextTracks {
	ct := &api.ContextTracks{URI: uri, Ready: true, Length: len(numbers), Cached: len(numbers)}
	for _, i := range numbers {
		ct.Tracks = append(ct.Tracks, api.ContextTrackItem{URI: trackURI(i), Track: &api.Track{
			URI: trackURI(i), Name: fmt.Sprint("T", i), ArtistNames: []string{"Band"}, Duration: 1000,
		}})
	}
	return ct
}

// onLikedSongs opens Liked Songs holding T0 … T5.
func onLikedSongs(t *testing.T, m Model) Model {
	t.Helper()
	m = press(m, "down", "right")
	if m.listURI() != likedSongsURI(testUsername) {
		t.Fatalf("not on Liked Songs but %q", m.listURI())
	}
	return update(m, listMsg{uri: m.listURI(), ct: tracksOf(m.listURI(), 0, 1, 2, 3, 4, 5)})
}

// pickSet chooses the playlist labelled name in the F menu, then "without"
// or "only".
func pickSet(t *testing.T, m Model, name string, only bool) Model {
	t.Helper()
	m = update(m, key("F"))
	if m.menu == nil {
		t.Fatalf("F must open the filter menu (err %v)", m.err)
	}
	for i, it := range m.menu.items {
		if it.label == name {
			m.menu.cursor = i
			m = update(m, key("enter"))
			if only {
				m = press(m, "down")
			}
			return update(m, key("enter"))
		}
	}
	t.Fatalf("no %q in the filter menu: %v", name, menuLabels(m.menu))
	return m
}

func menuLabels(mn *menu) []string {
	var out []string
	for _, it := range mn.items {
		out = append(out, it.label)
	}
	return out
}

func TestLikedSongsWithoutAPlaylist(t *testing.T) {
	m := onLikedSongs(t, newTestModel(t))
	m = pickSet(t, m, "Cool", false)

	if m.page().title != "− Cool" || len(m.rows()) != 0 {
		t.Fatalf("a new page narrowed by Cool must wait for Cool, title %q, rows %q", m.page().title, labels(m.rows()))
	}
	if got := m.emptyText(); got != "Loading Cool to compare with …" {
		t.Fatalf("empty text = %q", got)
	}

	cool := tracksOf(testCtx, 1, 3)
	cool.Cached = 0 // the uris are enough, metadata need not be there
	m = update(m, listMsg{uri: testCtx, ct: cool})
	if got := strings.Join(labels(m.rows()), "|"); got != "T0 — Band|T2 — Band|T4 — Band|T5 — Band" {
		t.Fatalf("Liked Songs − Cool = %q", got)
	}
	if m.rows()[1].index != 2 {
		t.Fatalf("rows keep their position in Liked Songs, got %d", m.rows()[1].index)
	}
	crumbs := ansi.Strip(m.renderBreadcrumb(100))
	if !strings.Contains(crumbs, "Liked Songs › − Cool") || !strings.Contains(crumbs, "4 of 6") {
		t.Fatalf("breadcrumb = %q", crumbs)
	}

	m = press(m, "left")
	if len(m.rows()) != 6 || len(m.page().sets) != 0 {
		t.Fatalf("← must return to all of Liked Songs")
	}
}

func TestOnlyTracksAlsoInAPlaylistAndChaining(t *testing.T) {
	m := onLikedSongs(t, newTestModel(t))
	m = update(m, listMsg{uri: testCtx, ct: tracksOf(testCtx, 1, 2, 3)})
	m = update(m, listMsg{uri: "spotify:playlist:theirs", ct: tracksOf("spotify:playlist:theirs", 2)})

	m = pickSet(t, m, "Cool", true)
	if got := strings.Join(labels(m.rows()), "|"); got != "T1 — Band|T2 — Band|T3 — Band" {
		t.Fatalf("Liked Songs ∩ Cool = %q", got)
	}

	m = update(m, key("F"))
	got := menuLabels(m.menu)
	if got[0] != "Add all 3 tracks to a playlist" || got[1] != "Queue all 3 tracks" {
		t.Fatalf("a narrowed page offers to act on what it shows, menu = %q", got)
	}
	for _, l := range got {
		if l == "Cool" || l == likedSongsName {
			t.Fatalf("listings already used are not offered again: %q", got)
		}
	}
	m.menu = nil

	m = pickSet(t, m, "Theirs", false)
	if got := strings.Join(labels(m.rows()), "|"); got != "T1 — Band|T3 — Band" {
		t.Fatalf("Liked Songs ∩ Cool − Theirs = %q", got)
	}
	if crumbs := ansi.Strip(m.renderBreadcrumb(100)); !strings.Contains(crumbs, "Liked Songs › ∩ Cool › − Theirs") {
		t.Fatalf("breadcrumb = %q", crumbs)
	}
}

func TestAddAllShownToAPlaylist(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedSongs(t, newTestModelWith(t, f.client(t)))
	m = update(m, listMsg{uri: testCtx, ct: tracksOf(testCtx, 1, 3)})
	m = pickSet(t, m, "Cool", false)

	m = update(m, key("F"))
	m = update(m, key("enter")) // Add all 4 tracks to a playlist
	if got := menuLabels(m.menu); strings.Join(got, "|") != "Cool|Party › Party One" {
		t.Fatalf("add all offers the editable playlists, menu = %q", got)
	}

	m = update(m, key("enter")) // Cool holds none of them: ask about all four
	if m.menu == nil || m.menu.title != "Add 4 tracks to Cool?" {
		t.Fatalf("adding many must ask first, menu = %+v", m.menu)
	}
	if _, sent := f.asked("/library/playlists/add_tracks"); sent {
		t.Fatalf("nothing may be added before the answer")
	}
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	body, sent := f.asked("/library/playlists/add_tracks")
	want := []any{trackURI(0), trackURI(2), trackURI(4), trackURI(5)}
	if !sent || body["playlist_uri"] != testCtx || fmt.Sprint(body["uris"]) != fmt.Sprint(want) {
		t.Fatalf("add all must send the shown tracks in one request, body = %v", body)
	}
	if m.note != "Added 4 tracks to Cool" {
		t.Fatalf("note = %q", m.note)
	}
}

func TestAddAllLeavesOutWhatThePlaylistHolds(t *testing.T) {
	m := onLikedSongs(t, newTestModel(t))
	m = update(m, listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1)})
	m = pickSet(t, m, "Cool", true) // T0, T1

	m = update(m, key("F"))
	m = update(m, key("enter"))
	m = update(m, key("enter")) // Cool
	if m.menu != nil || m.note != "Cool already holds all of them" {
		t.Fatalf("nothing to add must say so, note %q, menu %+v", m.note, m.menu)
	}
}

func TestQueueAllShown(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedSongs(t, newTestModelWith(t, f.client(t)))
	m = update(m, listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3)})
	m = pickSet(t, m, "Cool", false) // T4, T5

	m = update(m, key("F"))
	m = press(m, "down")
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if m.note != "Queued 2 tracks" {
		t.Fatalf("note = %q (err %v)", m.note, m.err)
	}
	var queued int
	for _, r := range f.requests {
		if strings.HasSuffix(r, "/player/add_to_queue") {
			queued++
		}
	}
	if queued != 2 {
		t.Fatalf("queued %d tracks, want 2: %q", queued, f.requests)
	}
}

func TestFilterNeedsATrackList(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("F"))
	if m.menu != nil || m.err == nil {
		t.Fatalf("F on the start page must explain, not open a menu")
	}
}
