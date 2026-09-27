package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// listURI is the context the top page lists, or "" when it is no track page.
func (m Model) listURI() string {
	p := m.page()
	switch {
	case p.kind != pageTracks:
		return ""
	case p.nowPlaying():
		return m.contextURI()
	}
	return p.uri
}

func (m Model) fetchList(uri string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ct, err := m.client.ContextTracks(ctx, uri)
		return listMsg{uri: uri, ct: ct, err: err}
	}
}

// syncList loads the top page's listing unless it is cached and complete.
func (m *Model) syncList() tea.Cmd {
	uri := m.listURI()
	if uri == "" {
		return nil
	}
	if ls := m.lists[uri]; ls != nil && ls.ct != nil && ls.ct.Complete() {
		return nil
	}
	if m.lists[uri] == nil {
		m.lists[uri] = &listState{}
	}
	return m.fetchList(uri)
}

func (m Model) applyList(msg listMsg) (tea.Model, tea.Cmd) {
	ls := m.lists[msg.uri]
	if ls == nil {
		ls = &listState{}
		m.lists[msg.uri] = ls
	}
	if msg.ct != nil && msg.ct.Ready && ls.ct != nil && ls.ct.Ready && msg.ct.Cached <= ls.ct.Cached {
		ls.stalls++
	} else {
		ls.stalls = 0
	}
	ls.ct, ls.err = msg.ct, msg.err
	if msg.uri != m.listURI() {
		return m, nil
	}

	m.followCursor(false)
	cmd := m.fetchLiked()
	if msg.ct != nil && !msg.ct.Complete() && ls.stalls < listMaxStalls {
		uri := msg.uri
		cmd = tea.Batch(cmd, tea.Tick(listPollInterval, func(time.Time) tea.Msg { return listPollMsg{uri: uri} }))
	}
	return m, cmd
}

// followCursor keeps the cursor on the playing track while the top page lists
// the playing context. It stops following once the user moves the cursor
// away and resumes when force is set or the cursor is back on the followed
// track.
func (m *Model) followCursor(force bool) {
	p := m.page()
	if p.kind != pageTracks || p.filter != "" || m.listURI() == "" || m.listURI() != m.contextURI() {
		return
	}
	rows := m.rows()
	onFollowed := m.followURI != "" && p.cursor < len(rows) && rows[p.cursor].uri == m.followURI
	if !force && !onFollowed && (m.followURI != "" || m.navigated) {
		return
	}
	cur := m.currentURI()
	for i, r := range rows {
		if r.uri == cur {
			p.cursor, m.followURI = i, cur
			centerOn(i, &p.offset, m.listHeight())
			return
		}
	}
}
