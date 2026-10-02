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
