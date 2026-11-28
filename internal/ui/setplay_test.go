package ui

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// onLikedWithoutCool opens Liked Songs − Cool, which shows T4 and T5.
func onLikedWithoutCool(t *testing.T, f *fakeDaemon) Model {
	t.Helper()
	m := onLikedSongs(t, newTestModelWith(t, f.client(t)))
	m = update(m, listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3)})
	return pickSet(t, m, "Cool", false)
}

func TestEnterPlaysOnlyTheFilteredTracks(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedWithoutCool(t, f)
	m = press(m, "down") // T5

	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	body, sent := f.asked("/player/play")
	if !sent || fmt.Sprint(body["uris"]) != fmt.Sprint([]any{trackURI(4), trackURI(5)}) || body["skip_to_uri"] != trackURI(5) {
		t.Fatalf("Enter must play the shown tracks from the selected one, body = %v", body)
	}
	if _, ok := body["uri"]; ok {
		t.Fatalf("a list plays without a context, body = %v", body)
	}

	// Until the daemon plays the list, it still reports Cool.
	if m.contextName() != "Cool" {
		t.Fatalf("context name before the switch = %q", m.contextName())
	}

	m = update(m, status("", 5))
	if got := m.contextName(); got != "Liked Songs − Cool" {
		t.Fatalf("the playing list is named by its filter, got %q", got)
	}
	if m.contextURI() != likedSongsURI(testUsername) {
		t.Fatalf("the list stands in for Liked Songs, context = %q", m.contextURI())
	}
	home := m.homeRows()[0]
	if !strings.HasPrefix(home.label, "Now playing") || len(home.open.sets) != 1 || home.open.sets[0].uri != testCtx {
		t.Fatalf("Now playing must list the filtered page again, row = %+v", home)
	}
	if !strings.Contains(strings.Join(m.renderPage(100), "\n"), "▶") {
		t.Fatalf("the playing track must be marked on the filtered page:\n%s", strings.Join(m.renderPage(100), "\n"))
	}

	// Playing a context again drops the list.
	m = update(m, status(testCtx, 1))
	if m.played != nil || m.contextName() != "Cool" {
		t.Fatalf("a context playing again must drop the list, name %q", m.contextName())
	}
}

func TestEnterOnAFilteredPageWithAnOldDaemon(t *testing.T) {
	f := &fakeDaemon{playErr: http.StatusBadRequest}
	m := onLikedWithoutCool(t, f)

	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	if m.err == nil || m.err.Error() != "the daemon cannot play only the filtered tracks" {
		t.Fatalf("err = %v", m.err)
	}
	if m.played != nil {
		t.Fatalf("a list that did not play must not stand in for the context")
	}
}

func TestEnterOnAPlainListPlaysTheContext(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedSongs(t, newTestModelWith(t, f.client(t)))

	nm, cmd := m.Update(key("enter"))
	_ = run(nm.(Model), cmd)
	body, sent := f.asked("/player/play")
	if !sent || body["uri"] != likedSongsURI(testUsername) || body["uris"] != nil {
		t.Fatalf("an unfiltered list plays its context, body = %v", body)
	}
}

// Adding a shown track to Cool takes it out of Liked Songs − Cool.
func TestAddingToTheComparedPlaylistUpdatesTheFilter(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedWithoutCool(t, f) // T4, T5, cursor on T4

	m = update(m, key("A"))
	m = press(m, "down") // past Liked Songs, onto Cool
	nm, cmd := m.Update(key("enter"))
	nm, cmd = nm.(Model).Update(cmd()) // the add answers
	m = nm.(Model)
	if cmd == nil || !m.lists[testCtx].stale {
		t.Fatalf("adding to Cool must load Cool again")
	}

	m = update(m, listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3, 4)})
	var got []string
	for _, r := range m.rows() {
		got = append(got, r.uri)
	}
	if fmt.Sprint(got) != fmt.Sprint([]string{trackURI(5)}) {
		t.Fatalf("rows = %v, want only T5", got)
	}
}

// A track added to Cool while Liked Songs − Cool plays leaves the list the
// daemon plays, even with the page no longer open.
func TestThePlayingListFollowsTheComparedPlaylist(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedWithoutCool(t, f) // T4, T5
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	m = update(m, status("", 4))
	m = press(m, "left", "left") // back home

	// T5 goes to Cool elsewhere in respot.
	nm, cmd = m.Update(libraryWriteMsg{note: "Added T5 to Cool", changed: testCtx})
	m = nm.(Model)
	if cmd == nil || !m.lists[testCtx].stale {
		t.Fatalf("Cool must load again while the list made of it plays")
	}
	nm, cmd = m.Update(listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3, 5)})
	m = run(nm.(Model), cmd)

	body, sent := f.asked("/player/update_list")
	if !sent || fmt.Sprint(body["uris"]) != fmt.Sprint([]any{trackURI(4)}) {
		t.Fatalf("the daemon must get the list without T5, body = %v", body)
	}
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}

	// Nothing changed: nothing is sent again.
	f.requests, f.bodies = nil, nil
	nm, cmd = m.Update(listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3, 5)})
	_ = run(nm.(Model), cmd)
	if _, again := f.asked("/player/update_list"); again {
		t.Fatalf("an unchanged list must not be sent again")
	}
}

// When the playing track itself goes to Cool, the list keeps it until it
// ends: the daemon gets what is left.
func TestThePlayingTrackLeavingTheList(t *testing.T) {
	f := &fakeDaemon{}
	m := onLikedWithoutCool(t, f)
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	m = update(m, status("", 4))

	m = update(m, libraryWriteMsg{changed: testCtx})
	nm, cmd = m.Update(listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3, 4)})
	m = run(nm.(Model), cmd)
	body, _ := f.asked("/player/update_list")
	if fmt.Sprint(body["uris"]) != fmt.Sprint([]any{trackURI(5)}) {
		t.Fatalf("body = %v", body)
	}

	// Everything in Cool: the list ends after the playing track.
	m = update(m, libraryWriteMsg{changed: testCtx})
	f.requests, f.bodies = nil, nil
	nm, cmd = m.Update(listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3, 4, 5)})
	_ = run(nm.(Model), cmd)
	body, _ = f.asked("/player/update_list")
	if fmt.Sprint(body["uris"]) != fmt.Sprint([]any{trackURI(4)}) {
		t.Fatalf("with nothing left only the playing track stays, body = %v", body)
	}
}

func TestAListThatNoLongerPlaysIsDropped(t *testing.T) {
	f := &fakeDaemon{updateErr: http.StatusConflict}
	m := onLikedWithoutCool(t, f)
	nm, cmd := m.Update(key("enter"))
	m = run(nm.(Model), cmd)
	m = update(m, status("", 4))

	m = update(m, libraryWriteMsg{changed: testCtx})
	nm, cmd = m.Update(listMsg{uri: testCtx, ct: tracksOf(testCtx, 0, 1, 2, 3, 5)})
	m = run(nm.(Model), cmd)
	if m.played != nil || m.err != nil {
		t.Fatalf("a list the daemon no longer plays must be dropped quietly, err %v", m.err)
	}
}
