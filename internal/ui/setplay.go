package ui

import (
	"context"
	"errors"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isnogudus/respot/internal/api"
)

// playedList is a filtered page whose tracks play as a list of their own.
// The daemon plays such a list without a context, so it stands in for the
// playing context: Now playing lists the page again, and the playing marker
// and the cursor follow on it.
type playedList struct {
	page page
	// pending is set until the daemon reports the list playing, while it
	// may still report the context that played before.
	pending bool
	// uris are the tracks the daemon was last given, to send the list again
	// only when the listings it is made of change what it holds.
	uris []string
}

// playListMsg answers playing a filtered page's tracks.
type playListMsg struct{ err error }

// playSetRow plays the tracks the filtered top page shows, in its order and
// with the daemon's shuffle, starting at r.
func (m Model) playSetRow(r row) (tea.Model, tea.Cmd) {
	p := m.page()
	uris := capURIs(addableURIs(m.rowsOf(p)), r.uri)
	played := *p
	played.cursor, played.offset, played.filter = 0, 0, ""
	if p.nowPlaying() {
		played.uri = m.contextURI()
	}
	m.played = &playedList{page: played, pending: true, uris: uris}
	m.followURI, m.navigated = r.uri, false

	c, skipTo := m.client, r.uri
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return playListMsg{err: c.PlayList(ctx, uris, skipTo)}
	}
}

func (m Model) applyPlayList(msg playListMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.played = nil
		if errors.Is(msg.err, api.ErrPlayListUnavailable) {
			m.setErr(errors.New("the daemon cannot play only the filtered tracks"))
		} else {
			m.setErr(msg.err)
		}
	}
	return m, m.fetchStatus()
}

// syncPlayed drops the played list once the daemon plays a context again.
func (m *Model) syncPlayed() {
	if m.played == nil || m.status == nil {
		return
	}
	if m.statusContextURI() == "" {
		m.played.pending = false
		return
	}
	if !m.played.pending {
		m.played = nil
	}
}

// playingList is the filtered page that plays as a list, or nil.
func (m Model) playingList() *page {
	if m.played == nil || m.played.pending || m.statusContextURI() != "" {
		return nil
	}
	return &m.played.page
}

// listName names a filtered page by its listing and its filters:
// "Liked Songs − Cool".
func (m Model) listName(p *page) string {
	parts := []string{orURI(m.nameOf(p.uri), p.uri)}
	for _, op := range p.sets {
		parts = append(parts, op.title())
	}
	return strings.Join(parts, " ")
}

// capURIs keeps at most api.MaxPlayURIs of uris, the ones from from on.
func capURIs(uris []string, from string) []string {
	if len(uris) <= api.MaxPlayURIs {
		return uris
	}
	at := max(0, slices.Index(uris, from))
	return uris[at:min(len(uris), at+api.MaxPlayURIs)]
}

// watchedLists are the listings kept loaded and fresh: those the top page
// shows or compares with, and those the filtered list playing is made of.
func (m Model) watchedLists() []string {
	out := m.pageLists()
	if m.played == nil {
		return out
	}
	for _, uri := range append([]string{m.played.page.uri}, setURIs(m.played.page.sets)...) {
		if !slices.Contains(out, uri) {
			out = append(out, uri)
		}
	}
	return out
}

func setURIs(sets []setOp) []string {
	out := make([]string, len(sets))
	for i, op := range sets {
		out[i] = op.uri
	}
	return out
}

// listUpdatedMsg answers sending the filtered list playing again.
type listUpdatedMsg struct{ err error }

// syncPlayingList sends the filtered list playing to the daemon again once
// the listings it is made of are loaded and change what it holds: a track
// added to Cool leaves Liked Songs − Cool while it plays. The playing track
// plays on even when it left the list.
func (m Model) syncPlayingList() tea.Cmd {
	pl := m.played
	if pl == nil || pl.pending {
		return nil
	}
	for _, uri := range append([]string{pl.page.uri}, setURIs(pl.page.sets)...) {
		if !m.listUsable(uri) {
			return nil
		}
	}
	rows, ok := m.applySets(&pl.page, m.trackRowsOf(pl.page.uri))
	if !ok {
		return nil
	}
	cur := m.currentURI()
	uris := capURIs(addableURIs(rows), cur)
	if len(uris) == 0 && isAddable(cur) {
		uris = []string{cur} // nothing is left: end after the playing track
	}
	if len(uris) == 0 || slices.Equal(uris, pl.uris) {
		return nil
	}
	pl.uris = uris

	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return listUpdatedMsg{err: c.UpdateList(ctx, uris)}
	}
}

func (m Model) applyListUpdated(msg listUpdatedMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err == nil:
	case errors.Is(msg.err, api.ErrNotPlayingList):
		m.played = nil // something else plays by now
	case errors.Is(msg.err, api.ErrPlayListUnavailable):
		m.setErr(errors.New("the daemon cannot update the playing list"))
	default:
		m.setErr(msg.err)
	}
	return m, nil
}
