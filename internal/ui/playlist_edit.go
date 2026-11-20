package ui

import (
	"context"
	"errors"
	"strings"

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
	switch {
	case ok && r.kind == rowTrack:
		return m.askRemove(m.rowRef(r)), nil
	case ok && m.page().kind == pagePlaylists && strings.HasPrefix(r.uri, "spotify:playlist:"):
		return m.askRemovePlaylist(r.uri), nil
	}
	m.setErr(errors.New("select a track or a playlist to remove"))
	return m, nil
}

// askRemovePlaylist asks before taking a playlist out of the library. For
// Spotify that deletes a playlist the user owns and unfollows anyone else's.
func (m Model) askRemovePlaylist(uri string) Model {
	var pl api.LibraryPlaylist
	for _, p := range m.library.playlists {
		if p.URI == uri {
			pl = p
		}
	}
	own := pl.OwnerUsername != "" && pl.OwnerUsername == m.username()
	title, subtitle, label, done := "Remove "+pl.Name+" from your library?",
		"Unfollows it; you can follow it again in Spotify.", "Remove", "Removed "+pl.Name+" from your library"
	if own {
		title, subtitle, label, done = "Delete "+pl.Name+"?",
			"Its followers keep it, and Spotify can restore it.", "Delete", "Deleted "+pl.Name
	}
	m.menu = confirmMenu(title, subtitle,
		menuItem{icon: "✕", label: label, run: func(m Model) (Model, tea.Cmd) {
			return m, m.removePlaylistCmd(uri, pl.Name, done)
		}})
	return m
}

func (m Model) removePlaylistCmd(uri, name, done string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		err := c.RemovePlaylist(ctx, uri)
		switch {
		case errors.Is(err, api.ErrPlaylistChanged):
			return libraryWriteMsg{err: errors.New("your library changed meanwhile; reloaded, try again"), playlistsChanged: true}
		case errors.Is(err, api.ErrNotInLibrary):
			return libraryWriteMsg{err: errors.New(name + " is not in your library any more, or the daemon cannot delete playlists"), playlistsChanged: true}
		case err != nil:
			return libraryWriteMsg{err: err}
		}
		return libraryWriteMsg{note: done, playlistsChanged: true}
	}
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

// createPlaylist creates an empty playlist at the top of the library.
func (m Model) createPlaylist(name string) (tea.Model, tea.Cmd) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.setErr(errors.New("a playlist needs a name"))
		return m, nil
	}
	c := m.client
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		uri, err := c.CreatePlaylist(ctx, name)
		switch {
		case errors.Is(err, api.ErrLibraryWriteUnavailable):
			return libraryWriteMsg{err: errors.New("the daemon cannot create playlists")}
		case errors.Is(err, api.ErrPlaylistChanged):
			return libraryWriteMsg{err: errors.New("created " + name + ", but your library kept changing; add it in Spotify"), playlistsChanged: true}
		case err != nil:
			return libraryWriteMsg{err: err}
		}
		return libraryWriteMsg{note: "Created " + name, playlistsChanged: true, created: uri}
	}
}

// cursorToPlaylist puts the cursor on a playlist when the playlists page it
// sits on lists it.
func (m *Model) cursorToPlaylist(uri string) {
	if m.page().kind != pagePlaylists {
		return
	}
	for i, r := range m.rows() {
		if r.uri == uri {
			p := m.page()
			p.cursor = i
			centerOn(i, &p.offset, m.listHeight())
			return
		}
	}
}
