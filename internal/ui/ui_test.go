package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const (
	testCtx      = "spotify:playlist:cool"
	testAlbum    = "spotify:album:alb"
	testArtist   = "spotify:artist:art"
	testUsername = "me"
)

func trackURI(i int) string { return fmt.Sprintf("spotify:track:%d", i) }

func update(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m = update(m, key(k))
	}
	return m
}

func status(ctxURI string, track int) statusMsg {
	st := &api.Status{Username: testUsername, DeviceName: "tron", DeviceType: "COMPUTER", VolumeSteps: 100}
	if ctxURI != "" {
		st.ContextURI = &ctxURI
	}
	if track >= 0 {
		st.Track = &api.Track{URI: trackURI(track), Name: fmt.Sprint("T", track), ArtistNames: []string{"Band"}, Duration: 1000}
	}
	return statusMsg{st: st}
}

func testTracks(uri string, n int) *api.ContextTracks {
	ct := &api.ContextTracks{URI: uri, Ready: true, Length: n, Cached: n}
	for i := range n {
		ct.Tracks = append(ct.Tracks, api.ContextTrackItem{URI: trackURI(i), Track: &api.Track{
			URI: trackURI(i), Name: fmt.Sprint("T", i), ArtistNames: []string{"Band"}, Duration: 1000,
			AlbumURI: testAlbum, AlbumName: "Album", ArtistURIs: []string{testArtist},
		}})
	}
	return ct
}

// newTestModel is playing track 1 of testCtx with a small library loaded.
func newTestModel(t *testing.T) Model {
	t.Helper()
	m := New(api.New("http://127.0.0.1:1")) // refuses connections
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(m, status(testCtx, 1))
	m = update(m, playlistsMsg{items: []api.LibraryPlaylist{
		{URI: testCtx, Name: "Cool", Length: 100, CanEdit: true},
		{URI: "spotify:playlist:p1", Name: "Party One", Folder: []string{"Party"}, CanEdit: true},
		{URI: "spotify:playlist:theirs", Name: "Theirs"},
		{URI: "spotify:playlist:p2", Name: "Party Deep", Folder: []string{"Party", "Old"}},
	}})
	m = update(m, albumsMsg{items: []api.LibraryAlbum{{URI: testAlbum, Name: "Album", ArtistNames: []string{"Band"}, Year: 1999}}})
	m = update(m, artistsMsg{items: []api.LibraryArtist{{URI: testArtist, Name: "Band"}}})
	return m
}

func labels(rows []row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.label
	}
	return out
}

func TestHomePage(t *testing.T) {
	m := newTestModel(t)
	got := labels(m.rows())
	want := []string{"Now playing · Cool", likedSongsName, "Playlists", "Albums", "Artists"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("home rows = %q, want %q", got, want)
	}
}

func TestBrowseIntoFoldersAndBack(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "down", "down", "right") // Playlists
	if got := labels(m.rows()); strings.Join(got, "|") != "Cool|Party|Theirs" {
		t.Fatalf("playlists = %q, want folders where their first playlist is", got)
	}
	if info := m.rows()[1].info; info != "2 playlists" {
		t.Fatalf("folder info = %q, want 2 playlists", info)
	}

	m = press(m, "down", "right") // Party
	if got := labels(m.rows()); strings.Join(got, "|") != "Party One|Old" {
		t.Fatalf("Party folder = %q", got)
	}
	if crumbs := ansi.Strip(m.renderBreadcrumb(100)); !strings.Contains(crumbs, "Start › Playlists › Party") {
		t.Fatalf("breadcrumb = %q", crumbs)
	}

	m = press(m, "left")
	if m.page().kind != pagePlaylists || m.page().cursor != 1 {
		t.Fatalf("back must restore the cursor on Party, got kind=%d cursor=%d", m.page().kind, m.page().cursor)
	}
	m = press(m, "left", "left")
	if len(m.stack) != 1 || m.page().cursor != 2 {
		t.Fatalf("back to start must keep its cursor, stack=%d cursor=%d", len(m.stack), m.page().cursor)
	}
}

func TestEnterPlaysAndRightOpens(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "down", "down", "right") // Playlists, cursor on Cool

	nm, cmd := m.Update(key("enter"))
	if cmd == nil || !strings.HasPrefix(nm.(Model).note, "Playing Cool") || len(nm.(Model).stack) != 2 {
		t.Fatalf("enter on a playlist must play it and stay on the page")
	}

	m = press(m, "right")
	if p := m.page(); p.kind != pageTracks || p.uri != testCtx || p.title != "Cool" {
		t.Fatalf("→ on a playlist must open its tracks, got %+v", *p)
	}
}

func TestShiftArrowsSeekInsteadOfNavigating(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "down", "down", "right")
	for _, k := range []string{"shift+left", "shift+right"} {
		nm, cmd := m.Update(key(k))
		if cmd == nil || len(nm.(Model).stack) != 2 {
			t.Fatalf("%s must seek (a command) and not navigate", k)
		}
	}
}

func TestFilterPerPage(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "down", "down", "right", "/")
	for _, r := range "part" {
		m = update(m, key(string(r)))
	}
	if got := labels(m.rows()); strings.Join(got, "|") != "Party" {
		t.Fatalf("filtered = %q", got)
	}
	m = press(m, "enter", "right") // keep filter, open Party
	if m.page().filter != "" || len(m.rows()) != 2 {
		t.Fatalf("a new page starts unfiltered")
	}
	m = press(m, "left")
	if m.page().filter != "part" {
		t.Fatalf("going back keeps the page's filter, got %q", m.page().filter)
	}
	m = press(m, "esc")
	if m.page().filter != "" {
		t.Fatalf("esc clears the filter")
	}
}

func TestFilterTypingDoesNotTriggerKeys(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "/", "q", "m", "n")
	if m.page().filter != "qmn" || m.mode != inputFilter {
		t.Fatalf("keys typed into the filter must filter, got %q", m.page().filter)
	}
}

func TestHomeAndNowPlayingKeys(t *testing.T) {
	m := newTestModel(t)
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 50)})
	m = press(m, "down", "down", "right", "down", "right")
	m = press(m, "m")
	if len(m.stack) != 1 {
		t.Fatalf("m must return to the start page")
	}
	m = press(m, "c")
	if !m.page().nowPlaying() || m.page().cursor != 1 {
		t.Fatalf("c must show now playing on the playing track, cursor=%d", m.page().cursor)
	}
}

func TestListsAreCachedAcrossNavigation(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "down", "down", "right")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 5)})
	nm, cmd := m.Update(key("right"))
	m = nm.(Model)
	if m.listURI() != testCtx || len(m.rows()) != 5 {
		t.Fatalf("an opened playlist shows its cached tracks")
	}
	_ = cmd
	if c := m.syncList(); c != nil {
		t.Fatalf("a complete cached list must not load again")
	}
}

func TestNowPlayingFollowsPlayback(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "c")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 100)})
	if m.page().cursor != 1 {
		t.Fatalf("cursor = %d, want on the playing track", m.page().cursor)
	}
	m = update(m, status(testCtx, 80))
	if m.page().cursor != 80 {
		t.Fatalf("cursor = %d, want to follow to 80", m.page().cursor)
	}
	m = press(m, "up", "up")
	m = update(m, status(testCtx, 90))
	if m.page().cursor != 78 {
		t.Fatalf("cursor = %d, want to stay where the user moved it", m.page().cursor)
	}
	m = press(m, "c")
	if m.page().cursor != 90 {
		t.Fatalf("c must jump back to the playing track, cursor = %d", m.page().cursor)
	}
}

func TestBrowsedListDoesNotFollowPlayback(t *testing.T) {
	m := newTestModel(t)
	m = update(m, listMsg{uri: testAlbum, ct: testTracks(testAlbum, 10)})
	m = press(m, "down", "down", "down", "right", "right") // Albums › Album
	if m.listURI() != testAlbum || m.page().cursor != 0 {
		t.Fatalf("album page: uri=%q cursor=%d", m.listURI(), m.page().cursor)
	}
}

func TestListStopsPollingWithoutProgress(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "c")
	partial := testTracks(testCtx, 100)
	partial.Cached = 90
	var cmd tea.Cmd
	for range listMaxStalls + 1 {
		var nm tea.Model
		nm, cmd = m.Update(listMsg{uri: testCtx, ct: partial})
		m = nm.(Model)
	}
	if m.lists[testCtx].stalls < listMaxStalls {
		t.Fatalf("stalls = %d", m.lists[testCtx].stalls)
	}
	_ = cmd
}

func openTrackMenu(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m = press(m, "c")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 10)})
	m = press(m, "down", "right") // track 2
	if m.menu == nil {
		t.Fatalf("→ on a track must open its menu")
	}
	return m
}

func TestTrackMenu(t *testing.T) {
	m := openTrackMenu(t)
	var got []string
	for _, it := range m.menu.items {
		got = append(got, it.label)
	}
	want := []string{"Play", "Add to queue", "Add to " + likedSongsName, "Add to playlist", "Go to album · Album", "Go to artist · Band"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("menu = %q, want %q", got, want)
	}

	m = press(m, "down", "down", "down", "right") // Add to playlist
	if m.menu == nil || m.menu.parent == nil {
		t.Fatalf("Add to playlist must open a submenu")
	}
	var names []string
	for _, it := range m.menu.items {
		names = append(names, it.label)
	}
	if strings.Join(names, "|") != "Cool|Party › Party One" {
		t.Fatalf("submenu = %q, want only editable playlists", names)
	}
	m = press(m, "left")
	if m.menu == nil || m.menu.title != "T2 — Band" {
		t.Fatalf("← must return to the track menu")
	}

	m = press(m, "down", "right") // Go to album
	if m.menu != nil || m.page().uri != testAlbum || m.page().title != "Album" {
		t.Fatalf("Go to album must open the album page, got %+v", *m.page())
	}
}

func TestMenuQDoesNotQuit(t *testing.T) {
	m := openTrackMenu(t)
	nm, cmd := m.Update(key("q"))
	if nm.(Model).menu != nil || cmd != nil {
		t.Fatalf("q must close the menu, not quit")
	}
}

func TestAddMenuTargetsPlayingTrackOffTrackPages(t *testing.T) {
	m := update(newTestModel(t), key("A"))
	if m.menu == nil || m.menu.subtitle != "T1 — Band" {
		t.Fatalf("A must offer to add the playing track, got %+v", m.menu)
	}
	if first := m.menu.items[0].label; first != likedSongsName {
		t.Fatalf("first entry = %q, want Liked Songs", first)
	}
}

func TestToggleLiked(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "c")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 10)})
	m = press(m, "down", "down", "down", "down")

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

func TestFetchLikedAsksForVisibleRowsOnce(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "c")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 100)})
	if !m.likedPending[trackURI(1)] || m.likedPending[trackURI(99)] {
		t.Fatalf("visible rows must be asked for, rows below the view not")
	}
	if cmd := m.fetchLiked(); cmd != nil {
		t.Fatalf("pending rows must not be asked for twice")
	}
	m = update(m, likedMsg{uris: []string{trackURI(1)}, states: map[string]bool{trackURI(1): true}})
	if liked, known := m.likedState(trackURI(1)); !liked || !known || m.likedPending[trackURI(1)] {
		t.Fatalf("answered state must be known and no longer pending")
	}
}

func TestLikedUnavailableStopsAsking(t *testing.T) {
	m := newTestModel(t)
	m = update(m, likedMsg{uris: []string{trackURI(1)}, err: api.ErrLikedUnavailable})
	m.likedPending = map[string]bool{}
	if cmd := m.fetchLiked(); cmd != nil {
		t.Fatalf("an old daemon must not be asked again")
	}
}

func TestViewLayout(t *testing.T) {
	cases := map[string]func(m Model) Model{
		"start":  func(m Model) Model { return m },
		"tracks": func(m Model) Model { return update(press(m, "c"), listMsg{uri: testCtx, ct: testTracks(testCtx, 100)}) },
		"note":   func(m Model) Model { m.setNote("Queued something"); return m },
		"prompt": func(m Model) Model { return press(m, "o") },
		"menu":   func(m Model) Model { return press(m, "A") },
		"help":   func(m Model) Model { return press(m, "?") },
	}
	for name, setup := range cases {
		for _, height := range []int{12, 30} {
			m := update(newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: height})
			m = setup(m)
			lines := strings.Split(m.View(), "\n")
			if len(lines) != height {
				t.Errorf("%s/%d: %d lines, want %d", name, height, len(lines), height)
			}
			last := ansi.Strip(lines[len(lines)-1])
			if !strings.Contains(last, "open") || !strings.HasSuffix(strings.TrimSpace(last), "quit") {
				t.Errorf("%s/%d: last line %q, want the whole key line", name, height, last)
			}
		}
	}
}

func TestHeartsAndPlayingMarkRendered(t *testing.T) {
	m := newTestModel(t)
	m = press(m, "c")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 10)})
	m = update(m, likedMsg{uris: []string{trackURI(1)}, states: map[string]bool{trackURI(1): true}})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "T1 ♥") {
		t.Fatalf("player must show the heart:\n%s", view)
	}
	found := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, " 2 T1 — Band") {
			found = true
			if !strings.Contains(line, "▶") || !strings.Contains(line, "♥") {
				t.Fatalf("playing liked row must carry ▶ and ♥: %q", line)
			}
		}
	}
	if !found {
		t.Fatalf("row of track 1 missing:\n%s", view)
	}
}

func TestContextNameFallsBackToLibrary(t *testing.T) {
	m := newTestModel(t)
	m = update(m, status(likedSongsURI(testUsername), -1))
	if got := m.contextName(); got != likedSongsName {
		t.Fatalf("context name = %q, want %q", got, likedSongsName)
	}
}

func TestNormalizeURI(t *testing.T) {
	cases := map[string]string{
		"spotify:track:5C1KYz8GLB8sGZYTpw3LpR":                             "spotify:track:5C1KYz8GLB8sGZYTpw3LpR",
		"  https://open.spotify.com/track/5C1KYz8GLB8sGZYTpw3LpR?si=abc  ": "spotify:track:5C1KYz8GLB8sGZYTpw3LpR",
		"https://open.spotify.com/intl-de/album/0sNOF9WDwhWunNAHPD3Baj":    "spotify:album:0sNOF9WDwhWunNAHPD3Baj",
	}
	for in, want := range cases {
		if got, err := normalizeURI(in); err != nil || got != want {
			t.Errorf("normalizeURI(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "foo", "https://example.com/track/x", "https://open.spotify.com/track"} {
		if _, err := normalizeURI(bad); err == nil {
			t.Errorf("normalizeURI(%q): expected error", bad)
		}
	}
}
