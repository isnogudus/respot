package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isnogudus/respot/internal/api"
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
// track rows that are neither known nor already asked for.
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
	if p := m.page(); p.kind == pageTracks {
		rows := m.rows()
		for i := p.offset; i < len(rows) && i < p.offset+m.listHeight(); i++ {
			want(rows[i].uri)
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
	t, ok := m.addTarget()
	if !ok || !isTrackURI(t.uri) {
		m.setErr(errors.New("no track to add to " + likedSongsName))
		return m, nil
	}
	liked, _ := m.likedState(t.uri)
	return m, m.setLikedCmd(t, !liked)
}

func (m Model) setLikedCmd(t trackRef, want bool) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		err := c.SetLiked(ctx, []string{t.uri}, want)
		if errors.Is(err, api.ErrLibraryWriteUnavailable) {
			err = errors.New("the daemon cannot change Liked Songs; it needs the library write endpoints")
		}
		note := "♥ Added " + t.title + " to " + likedSongsName
		if !want {
			note = "Removed " + t.title + " from " + likedSongsName
		}
		return libraryWriteMsg{note: note, err: err, likedURI: t.uri, liked: want}
	}
}
