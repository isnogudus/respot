package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isnogudus/respot/internal/api"
)

// fakeDaemon answers the playlist endpoints and records what it was asked.
type fakeDaemon struct {
	mu        sync.Mutex
	requests  []string // "METHOD path"
	bodies    []map[string]any
	contained bool
	noContain bool // answer /library/playlists/contains like an old daemon
	removeErr int  // status for remove_track, 0 for OK
	deleteErr int  // status for /library/playlists/remove, 0 for OK
	createErr int  // status for /library/playlists/create, 0 for OK
	playErr   int  // status for /player/play, 0 for OK
	updateErr int  // status for /player/update_list, 0 for OK
}

func (f *fakeDaemon) client(t *testing.T) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.bodies = append(f.bodies, decoded)
		f.mu.Unlock()

		switch r.URL.Path {
		case "/library/playlists/contains":
			if f.noContain {
				http.NotFound(w, r)
				return
			}
			uri := r.URL.Query().Get("uris")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"uri": uri, "contained": f.contained}}})
		case "/library/playlists/remove_track":
			if f.removeErr != 0 {
				w.WriteHeader(f.removeErr)
			}
		case "/library/playlists/remove":
			if f.deleteErr != 0 {
				w.WriteHeader(f.deleteErr)
			}
		case "/library/playlists/create":
			if f.createErr != 0 {
				w.WriteHeader(f.createErr)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"uri": "spotify:playlist:new"})
		case "/player/update_list":
			if f.updateErr != 0 {
				w.WriteHeader(f.updateErr)
			}
		case "/player/play":
			if f.playErr != 0 {
				w.WriteHeader(f.playErr)
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL)
}

func (f *fakeDaemon) asked(path string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.requests {
		if strings.HasSuffix(r, " "+path) {
			return f.bodies[i], true
		}
	}
	return nil, false
}

// run executes cmd and feeds its message back, as Bubble Tea would.
func run(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	return update(m, cmd())
}

// addToCool picks "Cool" in the add menu for the playing track.
func addToCool(t *testing.T, m Model) Model {
	t.Helper()
	m = update(m, key("A"))
	m = press(m, "down") // past Liked Songs, onto Cool
	nm, cmd := m.Update(key("enter"))
	return run(nm.(Model), cmd)
}

func TestAddAsksBeforeADuplicate(t *testing.T) {
	f := &fakeDaemon{contained: true}
	m := addToCool(t, newTestModelWith(t, f.client(t)))

	if m.menu == nil || m.menu.title != "Already in Cool" {
		t.Fatalf("a duplicate must be asked about, menu = %+v", m.menu)
	}
	if _, added := f.asked("/library/playlists/add_tracks"); added {
		t.Fatalf("nothing may be added before the answer")
	}

	nm, cmd := m.Update(key("enter")) // "Add it again"
	m = run(nm.(Model), cmd)
	if body, added := f.asked("/library/playlists/add_tracks"); !added || body["playlist_uri"] != testCtx {
		t.Fatalf("confirming must add the track again, body = %v", body)
	}
	if !strings.HasPrefix(m.note, "Added") {
		t.Fatalf("note = %q", m.note)
	}
}

func TestAddDuplicateCancelled(t *testing.T) {
	f := &fakeDaemon{contained: true}
	m := addToCool(t, newTestModelWith(t, f.client(t)))
	m = press(m, "down")
	nm, cmd := m.Update(key("enter")) // "Cancel"
	run(nm.(Model), cmd)
	if _, added := f.asked("/library/playlists/add_tracks"); added {
		t.Fatalf("cancel must not add")
	}
}

func TestAddWithoutDuplicateAddsAtOnce(t *testing.T) {
	for name, f := range map[string]*fakeDaemon{"new track": {}, "old daemon": {noContain: true}} {
		t.Run(name, func(t *testing.T) {
			m := addToCool(t, newTestModelWith(t, f.client(t)))
			if m.menu != nil {
				t.Fatalf("no question expected, menu = %q", m.menu.title)
			}
			if _, added := f.asked("/library/playlists/add_tracks"); !added {
				t.Fatalf("the track must be added")
			}
		})
	}
}

// coolWithTracks opens Now playing on Cool with five listed tracks and the
// cursor on the third.
func coolWithTracks(t *testing.T, f *fakeDaemon) Model {
	t.Helper()
	m := newTestModelWith(t, f.client(t))
	m = press(m, "c")
	m = update(m, listMsg{uri: testCtx, ct: testTracks(testCtx, 5)})
	m.page().cursor = 2
	return m
}

func TestRemoveAsksAndSendsThePosition(t *testing.T) {
	f := &fakeDaemon{}
	m := coolWithTracks(t, f)
	m = update(m, key("x"))
	if m.menu == nil || m.menu.title != "Remove from Cool?" {
		t.Fatalf("x must ask first, menu = %+v", m.menu)
	}

	nm, cmd := m.Update(key("enter")) // "Remove"
	m = run(nm.(Model), cmd)
	body, removed := f.asked("/library/playlists/remove_track")
	if !removed || body["uri"] != trackURI(2) || body["position"] != float64(2) || body["playlist_uri"] != testCtx {
		t.Fatalf("remove must name the entry, body = %v", body)
	}
	if !strings.HasPrefix(m.note, "Removed") || !m.lists[testCtx].stale {
		t.Fatalf("a removal must reload the playlist (note %q)", m.note)
	}
}

func TestRemoveFromAStaleListingReloads(t *testing.T) {
	f := &fakeDaemon{removeErr: http.StatusConflict}
	m := coolWithTracks(t, f)
	m = update(m, key("x"))
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if m.err == nil || !strings.Contains(m.err.Error(), "changed meanwhile") || !m.lists[testCtx].stale {
		t.Fatalf("a moved entry must be reported and the listing reloaded, err = %v", m.err)
	}
}

func TestRemoveOnlyFromOwnPlaylists(t *testing.T) {
	f := &fakeDaemon{}
	m := newTestModelWith(t, f.client(t))
	m = update(m, listMsg{uri: testAlbum, ct: testTracks(testAlbum, 3)})
	m = press(m, "down", "down", "down", "right", "right") // Albums › Album
	m = update(m, key("x"))
	if m.menu != nil || m.err == nil {
		t.Fatalf("an album is not a playlist of yours: no question, an error")
	}
}

// onPlaylist opens the Playlists page with the cursor on the playlist named
// name; Cool is owned by the user, Theirs by someone else.
func onPlaylist(t *testing.T, f *fakeDaemon, name string) Model {
	t.Helper()
	m := newTestModelWith(t, f.client(t))
	m.library.playlists[0].OwnerUsername = testUsername
	m.library.playlists[2].OwnerUsername = "someone"
	m = press(m, "down", "down", "right") // Playlists: Cool, Party, Theirs
	for i, r := range m.rows() {
		if r.label == name {
			m.page().cursor = i
			return m
		}
	}
	t.Fatalf("no playlist %q", name)
	return m
}

func TestDeleteOwnPlaylistAsksFirst(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Cool")
	m = update(m, key("x"))
	if m.menu == nil || m.menu.title != "Delete Cool?" {
		t.Fatalf("x on an own playlist must ask to delete it, menu = %+v", m.menu)
	}
	if _, sent := f.asked("/library/playlists/remove"); sent {
		t.Fatalf("nothing may be deleted before the answer")
	}

	nm, cmd := m.Update(key("enter")) // "Delete"
	m = run(nm.(Model), cmd)
	body, sent := f.asked("/library/playlists/remove")
	if !sent || body["playlist_uri"] != testCtx {
		t.Fatalf("delete must name the playlist, body = %v", body)
	}
	if m.note != "Deleted Cool" || !m.library.playlistsState.loading {
		t.Fatalf("a deletion must say so and list the playlists again (note %q)", m.note)
	}
}

func TestRemoveSomeoneElsesPlaylistFromTheLibrary(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Theirs")
	m = update(m, key("x"))
	if m.menu == nil || m.menu.title != "Remove Theirs from your library?" {
		t.Fatalf("x on a followed playlist must ask to remove it from the library, menu = %+v", m.menu)
	}
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if m.note != "Removed Theirs from your library" {
		t.Fatalf("note = %q", m.note)
	}
}

func TestDeletePlaylistCancelled(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Cool")
	m = update(m, key("x"))
	m = press(m, "down")
	nm, cmd := m.Update(key("enter")) // "Cancel"
	run(nm.(Model), cmd)
	if _, sent := f.asked("/library/playlists/remove"); sent {
		t.Fatalf("cancel must not delete")
	}
}

func TestDeleteAPlaylistGoneOrOnAnOldDaemon(t *testing.T) {
	f := &fakeDaemon{deleteErr: http.StatusNotFound}
	m := onPlaylist(t, f, "Cool")
	m = update(m, key("x"))
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if m.err == nil || !strings.Contains(m.err.Error(), "cannot delete playlists") || !m.library.playlistsState.loading {
		t.Fatalf("a 404 must be explained and the playlists listed again, err = %v", m.err)
	}
}

func TestOnlyPlaylistsAreDeleted(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Party") // a folder
	m = update(m, key("x"))
	if m.menu != nil || m.err == nil {
		t.Fatalf("a folder is not deleted: no question, an error")
	}

	m = newTestModelWith(t, f.client(t))
	m = press(m, "down") // home: Liked Songs
	m = update(m, key("x"))
	if m.menu != nil {
		t.Fatalf("Liked Songs on the home page is not deleted, menu = %q", m.menu.title)
	}
}

func typeText(m Model, s string) Model {
	return update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func TestCreatePlaylistLandsOnIt(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Theirs")
	m = update(m, key("N"))
	if m.mode != inputNewPlaylist {
		t.Fatalf("N must ask for the new playlist's name")
	}
	m = typeText(m, " Road Trip ")
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)

	body, sent := f.asked("/library/playlists/create")
	if !sent || body["name"] != "Road Trip" {
		t.Fatalf("create must send the trimmed name, body = %v", body)
	}
	if m.note != "Created Road Trip" || !m.library.playlistsState.loading {
		t.Fatalf("a creation must say so and list the playlists again (note %q)", m.note)
	}

	m = update(m, playlistsMsg{items: append([]api.LibraryPlaylist{{URI: "spotify:playlist:new", Name: "Road Trip"}}, m.library.playlists...)})
	if r, _ := m.selectedRow(); r.uri != "spotify:playlist:new" {
		t.Fatalf("the cursor must land on the new playlist, it is on %q", r.label)
	}
}

func TestCreatePlaylistNeedsAName(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Cool")
	m = update(m, key("N"))
	m = typeText(m, "   ")
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if _, sent := f.asked("/library/playlists/create"); sent || m.err == nil {
		t.Fatalf("an empty name must be an error, not a request (err %v)", m.err)
	}
}

func TestCreatePlaylistCancelled(t *testing.T) {
	f := &fakeDaemon{}
	m := onPlaylist(t, f, "Cool")
	m = update(m, key("N"))
	m = typeText(m, "Road Trip")
	nm, cmd := m.Update(key("esc"))
	m = run(nm.(Model), cmd)
	if _, sent := f.asked("/library/playlists/create"); sent || m.mode != inputNone {
		t.Fatalf("esc must cancel without creating")
	}
}

func TestCreatePlaylistOnAnOldDaemon(t *testing.T) {
	f := &fakeDaemon{createErr: http.StatusNotFound}
	m := onPlaylist(t, f, "Cool")
	m = update(m, key("N"))
	m = typeText(m, "Road Trip")
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if m.err == nil || !strings.Contains(m.err.Error(), "cannot create playlists") {
		t.Fatalf("a 404 must say the daemon cannot create playlists, err = %v", m.err)
	}
}
