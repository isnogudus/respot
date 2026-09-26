package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const likedSongsName = "Liked Songs"

// libraryEntry is one row of the library pane.
type libraryEntry struct {
	uri    string
	name   string
	folder []string
	length int // -1 when unknown
	liked  bool
}

// likedSongsURI is the context URI of the user's Liked Songs.
func likedSongsURI(username string) string {
	return "spotify:user:" + username + ":collection"
}

// libraryEntries lists Liked Songs followed by the library playlists. Liked
// Songs needs the username, so it only appears once the status is known.
func (m Model) libraryEntries() []libraryEntry {
	var out []libraryEntry
	if m.status != nil && m.status.Username != "" {
		out = append(out, libraryEntry{uri: likedSongsURI(m.status.Username), name: likedSongsName, length: -1, liked: true})
	}
	for _, p := range m.library {
		out = append(out, libraryEntry{uri: p.URI, name: p.Name, folder: p.Folder, length: p.Length})
	}
	return out
}

func (m Model) openLibrary() (tea.Model, tea.Cmd) {
	m.pane = paneLibrary
	if m.libLoading || (m.libLoaded && m.libErr == nil) {
		return m, nil
	}
	return m.reloadLibrary()
}

func (m Model) reloadLibrary() (tea.Model, tea.Cmd) {
	m.libLoading, m.libErr = true, nil
	return m, m.fetchLibrary()
}

func (m Model) selectedEntry() (libraryEntry, bool) {
	entries := m.libraryEntries()
	if m.libCursor < 0 || m.libCursor >= len(entries) {
		return libraryEntry{}, false
	}
	return entries[m.libCursor], true
}

// browseSelected opens the track list of the selected library entry without
// playing it; P plays the entry instead.
func (m Model) browseSelected() (tea.Model, tea.Cmd) {
	e, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}
	m.pane, m.browseURI, m.browseName = paneTracks, e.uri, e.name
	return m, m.syncList(true)
}

func (m Model) handleLibraryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.libraryEntries()
	if navigate(msg.String(), &m.libCursor, &m.libOffset, len(entries), m.listHeight()) {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+r":
		return m.reloadLibrary()
	case "c":
		cur := m.contextURI()
		for i, e := range entries {
			if e.uri == cur {
				m.libCursor = i
				centerOn(i, &m.libOffset, m.listHeight())
				break
			}
		}
	case "enter":
		return m.browseSelected()
	case "P":
		e, ok := m.selectedEntry()
		if !ok {
			return m, nil
		}
		m.note, m.noteAt = "Playing "+e.name, time.Now()
		return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, e.uri, "") })
	}
	return m, nil
}

func (m Model) renderLibrary(rows int) string {
	w := m.contentWidth()
	header := " " + titleStyle.Render("Library")
	if m.libLoaded {
		header += mutedStyle.Render(fmt.Sprintf(" · %d playlists", len(m.library)))
	}

	header = paneHeader(header, "enter open · P play · esc close", w)

	switch {
	case m.libErr != nil && errors.Is(m.libErr, api.ErrLibraryUnavailable):
		return header + "\n" + errStyle.Render(" The daemon does not provide /library/playlists.") + "\n" +
			subtleStyle.Render(" It needs a go-librespot build with the library endpoint.")
	case m.libErr != nil:
		return header + "\n" + errStyle.Render(ansi.Truncate(" "+m.libErr.Error(), w, "…")) + "\n" +
			subtleStyle.Render(" Press ctrl+r to retry.")
	case !m.libLoaded:
		return header + "\n" + subtleStyle.Render(" Loading library …")
	}

	entries := m.libraryEntries()
	scrollTo(m.libCursor, &m.libOffset, rows)
	cur := m.contextURI()
	lines := []string{header}
	for i := m.libOffset; i < len(entries) && i < m.libOffset+rows; i++ {
		lines = append(lines, renderLibraryRow(entries[i], i == m.libCursor, entries[i].uri == cur, w))
	}
	return strings.Join(lines, "\n")
}

func renderLibraryRow(e libraryEntry, selected, playing bool, w int) string {
	mark := "  "
	if playing {
		mark = accentStyle.Render("▶ ")
	}
	icon := subtleStyle.Render("≡ ")
	if e.liked {
		icon = accentStyle.Render("♥ ")
	}

	title := e.name
	if playing {
		title = accentStyle.Render(title)
	}
	if len(e.folder) > 0 {
		title = subtleStyle.Render(strings.Join(e.folder, " › ")+" › ") + title
	}

	info := ""
	if e.length >= 0 {
		info = fmt.Sprintf("%d tracks", e.length)
	}

	prefix := " " + mark + icon
	avail := w - lipgloss.Width(prefix) - len(info) - 3
	title = ansi.Truncate(title, max(5, avail), "…")
	pad := max(1, w-1-lipgloss.Width(prefix)-lipgloss.Width(title)-len(info))
	row := prefix + title + strings.Repeat(" ", pad) + mutedStyle.Render(info) + " "
	if selected {
		row = cursorStyle.Width(w).Render(row)
	}
	return row
}
