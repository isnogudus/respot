package ui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const likedSongsName = "Liked Songs"

type (
	playlistsMsg struct {
		items []api.LibraryPlaylist
		err   error
	}
	albumsMsg struct {
		items []api.LibraryAlbum
		err   error
	}
	artistsMsg struct {
		items []api.LibraryArtist
		err   error
	}
)

// loadState tracks one of the library listings.
type loadState struct {
	loaded  bool
	loading bool
	err     error
}

// ready reports whether the listing can be shown.
func (s loadState) ready() bool { return s.loaded && s.err == nil }

// libraryData holds the user's playlists, saved albums and followed artists.
type libraryData struct {
	playlists      []api.LibraryPlaylist
	albums         []api.LibraryAlbum
	artists        []api.LibraryArtist
	playlistsState loadState
	albumsState    loadState
	artistsState   loadState
}

func (d *libraryData) apply(msg tea.Msg) {
	switch msg := msg.(type) {
	case playlistsMsg:
		d.playlistsState = loadState{loaded: msg.err == nil, err: msg.err}
		if msg.err == nil {
			d.playlists = msg.items
		}
	case albumsMsg:
		d.albumsState = loadState{loaded: msg.err == nil, err: msg.err}
		if msg.err == nil {
			d.albums = msg.items
		}
	case artistsMsg:
		d.artistsState = loadState{loaded: msg.err == nil, err: msg.err}
		if msg.err == nil {
			d.artists = msg.items
		}
	}
}

// likedSongsURI is the context URI of the user's Liked Songs.
func likedSongsURI(username string) string {
	return "spotify:user:" + username + ":collection"
}

func libraryFetch[T any](fetch func(context.Context) (T, error), wrap func(T, error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*requestTimeout)
		defer cancel()
		return wrap(fetch(ctx))
	}
}

func (m *Model) fetchPlaylists() tea.Cmd {
	m.library.playlistsState.loading = true
	return libraryFetch(m.client.LibraryPlaylists, func(v []api.LibraryPlaylist, err error) tea.Msg { return playlistsMsg{v, err} })
}

func (m *Model) fetchAlbums() tea.Cmd {
	m.library.albumsState.loading = true
	return libraryFetch(m.client.LibraryAlbums, func(v []api.LibraryAlbum, err error) tea.Msg { return albumsMsg{v, err} })
}

func (m *Model) fetchArtists() tea.Cmd {
	m.library.artistsState.loading = true
	return libraryFetch(m.client.LibraryArtists, func(v []api.LibraryArtist, err error) tea.Msg { return artistsMsg{v, err} })
}

func (m *Model) reloadLibrary() tea.Cmd {
	m.setNote("Reloading library …")
	return tea.Batch(m.fetchPlaylists(), m.fetchAlbums(), m.fetchArtists())
}

// libraryErrText explains a failed library listing.
func libraryErrText(err error) string {
	if errors.Is(err, api.ErrLibraryUnavailable) {
		return "The daemon does not provide this listing; it needs a go-librespot build with the library endpoints."
	}
	return err.Error() + " — press ctrl+r to retry."
}

// contextName names the playing context: the daemon's name for it, else the
// name it has in the library. The daemon gives none for Liked Songs.
func (m Model) contextName() string {
	if m.status != nil && m.status.ContextName != nil && *m.status.ContextName != "" {
		return *m.status.ContextName
	}
	return m.nameOf(m.contextURI())
}

// nameOf names a context URI from the library, or "".
func (m Model) nameOf(uri string) string {
	if uri == "" {
		return ""
	}
	if uri == likedSongsURI(m.username()) {
		return likedSongsName
	}
	for _, p := range m.library.playlists {
		if p.URI == uri {
			return p.Name
		}
	}
	for _, a := range m.library.albums {
		if a.URI == uri {
			return a.Name
		}
	}
	for _, a := range m.library.artists {
		if a.URI == uri {
			return a.Name
		}
	}
	return ""
}
