package ui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const (
	popupMaxWidth = 64
	popupMaxRows  = 12
)

// addPopup is the "Add to …" dialog for one track.
type addPopup struct {
	uri    string
	title  string
	cursor int
	offset int
}

// libraryWriteMsg reports the outcome of adding a track somewhere.
type libraryWriteMsg struct {
	note string
	err  error

	// likedURI is set when the write changed a track's liked state to liked.
	likedURI string
	liked    bool
}

// addTarget is the track an add acts on: the one under the cursor in a track
// list, otherwise the playing one.
func (m Model) addTarget() (uri, title string, ok bool) {
	if m.pane == paneTracks && m.list != nil && m.cursor < len(m.list.Tracks) {
		it := m.list.Tracks[m.cursor]
		return it.URI, trackTitle(it), isAddable(it.URI)
	}
	if m.status != nil && m.status.Track != nil {
		t := m.status.Track
		return t.URI, t.Name + " — " + strings.Join(t.ArtistNames, ", "), isAddable(t.URI)
	}
	return "", "", false
}

func isAddable(uri string) bool {
	return strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:")
}

// popupEntries are the places a track can be added to: Liked Songs and the
// playlists the user may edit.
func (m Model) popupEntries() []libraryEntry {
	var out []libraryEntry
	if m.status != nil && m.status.Username != "" {
		out = append(out, libraryEntry{uri: likedSongsURI(m.status.Username), name: likedSongsName, length: -1, liked: true})
	}
	for _, p := range m.library {
		if p.CanEdit {
			out = append(out, libraryEntry{uri: p.URI, name: p.Name, folder: p.Folder, length: p.Length})
		}
	}
	return out
}

func (m Model) openAddPopup() (tea.Model, tea.Cmd) {
	uri, title, ok := m.addTarget()
	if !ok {
		m.setErr(errors.New("no track to add"))
		return m, nil
	}
	m.popup = &addPopup{uri: uri, title: title}
	if m.libLoaded || m.libLoading {
		return m, nil
	}
	return m.reloadLibrary()
}

func (m Model) addTo(e libraryEntry, uri, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		var err error
		if e.liked {
			err = m.client.SetLiked(ctx, []string{uri}, true)
		} else {
			err = m.client.AddToPlaylist(ctx, e.uri, []string{uri})
		}
		if errors.Is(err, api.ErrLibraryWriteUnavailable) {
			err = errors.New("the daemon cannot add to the library; it needs the library write endpoints")
		}
		msg := libraryWriteMsg{note: "Added " + title + " to " + e.name, err: err}
		if e.liked {
			msg.likedURI, msg.liked = uri, true
		}
		return msg
	}
}

func (m Model) handlePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := *m.popup
	entries := m.popupEntries()
	if navigate(msg.String(), &p.cursor, &p.offset, len(entries), popupMaxRows) {
		m.popup = &p
		return m, nil
	}
	switch msg.String() {
	case "esc", "q", "A":
		m.popup = nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		if p.cursor >= len(entries) {
			return m, nil
		}
		m.popup = nil
		return m, m.addTo(entries[p.cursor], p.uri, p.title)
	}
	return m, nil
}

func (m Model) renderPopup() string {
	p := *m.popup
	w := min(popupMaxWidth, m.contentWidth()-4)
	inner := w - 4

	lines := []string{
		titleStyle.Render("Add to …"),
		mutedStyle.Render(ansi.Truncate(p.title, inner, "…")),
		"",
	}
	entries := m.popupEntries()
	switch {
	case m.libErr != nil:
		lines = append(lines, errStyle.Render(ansi.Truncate(m.libErr.Error(), inner, "…")))
	case !m.libLoaded:
		lines = append(lines, subtleStyle.Render("Loading playlists …"))
	}
	scrollTo(p.cursor, &p.offset, popupMaxRows)
	for i := p.offset; i < len(entries) && i < p.offset+popupMaxRows; i++ {
		e := entries[i]
		icon := subtleStyle.Render("≡ ")
		if e.liked {
			icon = accentStyle.Render("♥ ")
		}
		name := e.name
		if liked, _ := m.likedState(p.uri); e.liked && liked {
			name += subtleStyle.Render("  (already liked)")
		}
		if len(e.folder) > 0 {
			name = subtleStyle.Render(strings.Join(e.folder, " › ")+" › ") + name
		}
		row := " " + icon + ansi.Truncate(name, inner-4, "…")
		if i == p.cursor {
			row = cursorStyle.Width(inner).Render(row)
		}
		lines = append(lines, row)
	}
	lines = append(lines, "", subtleStyle.Render("enter add · esc cancel"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(green).
		Padding(0, 1).
		Width(w - 2).
		Render(strings.Join(lines, "\n"))
}

// overlay draws fg over the lines of bg with its top left corner at x, y.
func overlay(bg []string, fg string, x, y int) []string {
	for i, line := range strings.Split(fg, "\n") {
		row := y + i
		if row < 0 || row >= len(bg) {
			continue
		}
		left := ansi.Truncate(bg[row], x, "")
		if gap := x - ansi.StringWidth(left); gap > 0 {
			left += strings.Repeat(" ", gap)
		}
		right := ansi.TruncateLeft(bg[row], x+ansi.StringWidth(line), "")
		bg[row] = left + "\x1b[0m" + line + "\x1b[0m" + right
	}
	return bg
}
