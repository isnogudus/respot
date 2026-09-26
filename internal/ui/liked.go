package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

// likedTTL is how long known liked states are trusted before they are asked
// for again, so changes made on other devices show up.
const likedTTL = 2 * time.Minute

// likedMsg carries liked states for a batch of track URIs.
type likedMsg struct {
	uris   []string
	states map[string]bool
	err    error
}

func isTrackURI(uri string) bool { return strings.HasPrefix(uri, "spotify:track:") }

// likedState returns whether uri is liked and whether that is known.
func (m Model) likedState(uri string) (liked, known bool) {
	liked, known = m.liked[uri]
	return liked, known
}

// fetchLiked asks for the liked state of the playing track and the visible
// rows of the track list that are not known or already asked for.
func (m *Model) fetchLiked() tea.Cmd {
	if m.likedUnavailable || m.status == nil {
		return nil
	}
	if m.liked == nil || time.Since(m.likedAt) > likedTTL {
		m.liked, m.likedPending, m.likedAt = map[string]bool{}, map[string]bool{}, time.Now()
	}

	var uris []string
	want := func(uri string) {
		if len(uris) >= api.MaxLikedQuery || !isTrackURI(uri) || m.likedPending[uri] {
			return
		}
		if _, known := m.liked[uri]; known {
			return
		}
		m.likedPending[uri] = true
		uris = append(uris, uri)
	}

	want(m.currentURI())
	if m.pane == paneTracks && m.list != nil {
		rows := m.listHeight()
		for i := m.offset; i < len(m.list.Tracks) && i < m.offset+rows; i++ {
			want(m.list.Tracks[i].URI)
		}
	}
	if len(uris) == 0 {
		return nil
	}

	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		states, err := c.Liked(ctx, uris)
		return likedMsg{uris: uris, states: states, err: err}
	}
}

func (m Model) applyLiked(msg likedMsg) (tea.Model, tea.Cmd) {
	for _, uri := range msg.uris {
		delete(m.likedPending, uri)
	}
	switch {
	case errors.Is(msg.err, api.ErrLikedUnavailable):
		m.likedUnavailable = true
		return m, nil
	case msg.err != nil:
		return m, nil // shown as unknown; asked again on the next change
	}
	for uri, liked := range msg.states {
		m.liked[uri] = liked
	}
	return m, nil
}

// toggleLiked adds the target track to Liked Songs, or removes it when it is
// known to be liked already.
func (m Model) toggleLiked() (tea.Model, tea.Cmd) {
	uri, title, ok := m.addTarget()
	if !ok || !isTrackURI(uri) {
		m.setErr(errors.New("no track to add to " + likedSongsName))
		return m, nil
	}
	liked, _ := m.likedState(uri)
	want := !liked

	c := m.client
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		err := c.SetLiked(ctx, []string{uri}, want)
		if errors.Is(err, api.ErrLibraryWriteUnavailable) {
			err = errors.New("the daemon cannot change Liked Songs; it needs the library write endpoints")
		}
		note := "♥ Added " + title + " to " + likedSongsName
		if !want {
			note = "Removed " + title + " from " + likedSongsName
		}
		return libraryWriteMsg{note: note, err: err, likedURI: uri, liked: want}
	}
}
