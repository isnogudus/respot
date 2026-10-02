package ui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isnogudus/respot/internal/api"
)

// duplicateMsg reports that a playlist already holds the track to add.
type duplicateMsg struct {
	t           trackRef
	playlistURI string
	name        string
}

// confirmMenu asks a yes/no question; only yes acts.
func confirmMenu(title, subtitle string, yes menuItem) *menu {
	return &menu{title: title, subtitle: subtitle, items: []menuItem{
		yes,
		{icon: "←", label: "Cancel", run: func(m Model) (Model, tea.Cmd) { return m, nil }},
	}}
}

// checkAndAddCmd adds t to a playlist unless the playlist already holds it,
// in which case it asks first. Daemons that cannot tell just add.
func (m Model) checkAndAddCmd(t trackRef, playlistURI, name string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		held, err := c.PlaylistContains(ctx, playlistURI, []string{t.uri})
		switch {
		case err == nil && held[t.uri]:
			return duplicateMsg{t: t, playlistURI: playlistURI, name: name}
		case err != nil && !errors.Is(err, api.ErrContainsUnavailable):
			return libraryWriteMsg{err: err}
		}
		return m.addToPlaylistCmd(t, playlistURI, name)()
	}
}

func (m Model) askAddAgain(msg duplicateMsg) Model {
	t, uri, name := msg.t, msg.playlistURI, msg.name
	m.menu = confirmMenu("Already in "+name, t.title+" is already in "+name+". Add it again?",
		menuItem{icon: "+", label: "Add it again", run: func(m Model) (Model, tea.Cmd) {
			return m, m.addToPlaylistCmd(t, uri, name)
		}})
	return m
}

// editablePlaylist names the playlist uri when the user may edit it.
func (m Model) editablePlaylist(uri string) (string, bool) {
	for _, p := range m.library.playlists {
		if p.URI == uri && p.CanEdit {
			return p.Name, true
		}
	}
	return "", false
}

// removable reports whether t can be removed from the playlist it is listed
// in, and that playlist's name.
func (m Model) removable(t trackRef) (string, bool) {
	if t.position < 0 || !isAddable(t.uri) {
		return "", false
	}
	return m.editablePlaylist(t.contextURI)
}

// askRemove asks before removing t from the playlist it is listed in.
func (m Model) askRemove(t trackRef) Model {
	name, ok := m.removable(t)
	if !ok {
		m.setErr(errors.New("select a track of one of your own playlists to remove it"))
		return m
	}
	m.menu = confirmMenu("Remove from "+name+"?", t.title,
		menuItem{icon: "✕", label: "Remove", run: func(m Model) (Model, tea.Cmd) {
			return m, m.removeCmd(t, name)
		}})
	return m
}

func (m Model) removeSelected() (tea.Model, tea.Cmd) {
	r, ok := m.selectedRow()
	if !ok || r.kind != rowTrack {
		m.setErr(errors.New("select a track to remove"))
		return m, nil
	}
	return m.askRemove(m.rowRef(r)), nil
}

func (m Model) removeCmd(t trackRef, name string) tea.Cmd {
	c, playlistURI := m.client, t.contextURI
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		err := c.RemoveFromPlaylist(ctx, playlistURI, t.uri, t.position)
		switch {
		case errors.Is(err, api.ErrPlaylistChanged), errors.Is(err, api.ErrNotInPlaylist):
			// The listing is out of date: say so and list the playlist again.
			return libraryWriteMsg{err: errors.New(name + " changed meanwhile; reloaded, try again"), changed: playlistURI}
		case err != nil:
			return libraryWriteMsg{err: err}
		}
		return libraryWriteMsg{note: "Removed " + t.title + " from " + name, changed: playlistURI, playlistsChanged: true}
	}
}
