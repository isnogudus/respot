package ui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/isnogudus/respot/internal/api"
)

const (
	menuMaxWidth = 64
	menuMaxRows  = 12
)

// menu is a popup list of actions. → or Enter runs an item, ← returns to the
// parent menu, esc closes.
type menu struct {
	title    string
	subtitle string
	items    []menuItem
	cursor   int
	offset   int
	parent   *menu
}

type menuItem struct {
	icon  string
	label string
	sub   bool // opens a submenu or a page, shown with →
	// run performs the item and returns the model and command; it may set
	// m.menu to open a submenu or leave it nil to close.
	run func(m Model) (Model, tea.Cmd)
}

// libraryWriteMsg reports the outcome of adding a track somewhere.
type libraryWriteMsg struct {
	note string
	err  error

	// likedURI is set when the write changed a track's liked state.
	likedURI string
	liked    bool
}

// trackRef is a track an action menu acts on.
type trackRef struct {
	uri        string
	title      string
	contextURI string // context to play it in, "" for none
	albumURI   string
	albumName  string
	artistURIs []string
	artists    []string
}

func refFromTrack(t *api.Track, uri, contextURI string) trackRef {
	ref := trackRef{uri: uri, title: uri, contextURI: contextURI}
	if t != nil {
		ref.title = t.Name + " — " + strings.Join(t.ArtistNames, ", ")
		ref.albumURI, ref.albumName = t.AlbumURI, t.AlbumName
		ref.artistURIs, ref.artists = t.ArtistURIs, t.ArtistNames
	}
	return ref
}

// addTarget is the track an action acts on: the selected row on a track
// page, otherwise the playing track.
func (m Model) addTarget() (trackRef, bool) {
	if r, ok := m.selectedRow(); ok && r.kind == rowTrack {
		return refFromTrack(r.track.Track, r.uri, m.listURI()), isAddable(r.uri)
	}
	if m.status != nil && m.status.Track != nil {
		return refFromTrack(m.status.Track, m.status.Track.URI, m.contextURI()), isAddable(m.status.Track.URI)
	}
	return trackRef{}, false
}

func isAddable(uri string) bool {
	return strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:")
}

func (m Model) openTrackMenu(r row) (tea.Model, tea.Cmd) {
	var t *api.Track
	if r.track != nil {
		t = r.track.Track
	}
	m.menu = m.trackMenu(refFromTrack(t, r.uri, m.listURI()))
	return m, nil
}

func (m Model) trackMenu(t trackRef) *menu {
	mn := &menu{title: t.title}
	mn.items = append(mn.items, menuItem{icon: "▶", label: "Play", run: func(m Model) (Model, tea.Cmd) {
		m.followURI, m.navigated = t.uri, false
		if t.contextURI == "" {
			return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, t.uri, "") })
		}
		return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, t.contextURI, t.uri) })
	}})
	mn.items = append(mn.items, menuItem{icon: "+", label: "Add to queue", run: func(m Model) (Model, tea.Cmd) {
		m.setNote("Queued: " + t.title)
		return m, m.action(func(ctx context.Context) error { return m.client.AddToQueue(ctx, t.uri) })
	}})
	if isTrackURI(t.uri) {
		label := "Add to " + likedSongsName
		if liked, _ := m.likedState(t.uri); liked {
			label = "Remove from " + likedSongsName
		}
		mn.items = append(mn.items, menuItem{icon: "♥", label: label, run: func(m Model) (Model, tea.Cmd) {
			return m, m.setLikedCmd(t, !m.liked[t.uri])
		}})
	}
	mn.items = append(mn.items, menuItem{icon: "≡", label: "Add to playlist", sub: true, run: func(m Model) (Model, tea.Cmd) {
		m.menu = m.playlistMenu(t)
		return m, nil
	}})
	if t.albumURI != "" {
		mn.items = append(mn.items, menuItem{icon: "◎", label: "Go to album · " + t.albumName, sub: true, run: func(m Model) (Model, tea.Cmd) {
			nm, cmd := m.push(page{kind: pageTracks, title: t.albumName, uri: t.albumURI})
			return nm.(Model), cmd
		}})
	}
	for i, uri := range t.artistURIs {
		if uri == "" || i >= len(t.artists) {
			continue
		}
		name := t.artists[i]
		mn.items = append(mn.items, menuItem{icon: "☺", label: "Go to artist · " + name, sub: true, run: func(m Model) (Model, tea.Cmd) {
			nm, cmd := m.push(page{kind: pageTracks, title: name, uri: uri})
			return nm.(Model), cmd
		}})
	}
	return mn
}

// playlistMenu lists the playlists a track can be added to.
func (m Model) playlistMenu(t trackRef) *menu {
	mn := &menu{title: "Add to playlist", subtitle: t.title}
	for _, p := range m.library.playlists {
		if !p.CanEdit {
			continue
		}
		p := p
		label := p.Name
		if len(p.Folder) > 0 {
			label = strings.Join(p.Folder, " › ") + " › " + p.Name
		}
		mn.items = append(mn.items, menuItem{icon: "≡", label: label, run: func(m Model) (Model, tea.Cmd) {
			return m, m.addToPlaylistCmd(t, p.URI, p.Name)
		}})
	}
	return mn
}

// openAddMenu opens "Add to …" for the target track: Liked Songs and the
// playlists the user may edit.
func (m Model) openAddMenu() (tea.Model, tea.Cmd) {
	t, ok := m.addTarget()
	if !ok {
		m.setErr(errors.New("no track to add"))
		return m, nil
	}
	mn := m.playlistMenu(t)
	mn.title = "Add to …"
	if isTrackURI(t.uri) {
		label := likedSongsName
		if liked, _ := m.likedState(t.uri); liked {
			label += "  (already liked)"
		}
		mn.items = append([]menuItem{{icon: "♥", label: label, run: func(m Model) (Model, tea.Cmd) {
			return m, m.setLikedCmd(t, true)
		}}}, mn.items...)
	}
	m.menu = mn
	return m, nil
}

func (m Model) addToPlaylistCmd(t trackRef, playlistURI, name string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		err := c.AddToPlaylist(ctx, playlistURI, []string{t.uri})
		if errors.Is(err, api.ErrLibraryWriteUnavailable) {
			err = errors.New("the daemon cannot add to playlists; it needs the library write endpoints")
		}
		return libraryWriteMsg{note: "Added " + t.title + " to " + name, err: err}
	}
}

func (m Model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	mn := *m.menu
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.menu = nil
		return m, nil
	case "left", "h", "backspace":
		m.menu = mn.parent
		return m, nil
	case "right", "l", "enter":
		if mn.cursor >= len(mn.items) {
			return m, nil
		}
		item := mn.items[mn.cursor]
		m.menu = nil
		nm, cmd := item.run(m)
		if item.sub && nm.menu != nil {
			nm.menu.parent = &mn // ← returns to this menu as it is now
		}
		return nm, cmd
	}
	if navigate(msg.String(), &mn.cursor, &mn.offset, len(mn.items), menuMaxRows) {
		m.menu = &mn
	}
	return m, nil
}

func (m Model) renderMenu() string {
	mn := *m.menu
	w := min(menuMaxWidth, m.contentWidth()-4)
	inner := w - 4

	lines := []string{titleStyle.Render(ansi.Truncate(mn.title, inner, "…"))}
	if mn.subtitle != "" {
		lines = append(lines, mutedStyle.Render(ansi.Truncate(mn.subtitle, inner, "…")))
	}
	lines = append(lines, "")
	if len(mn.items) == 0 {
		lines = append(lines, subtleStyle.Render("Nothing to choose from."))
	}
	scrollTo(mn.cursor, &mn.offset, menuMaxRows)
	for i := mn.offset; i < len(mn.items) && i < mn.offset+menuMaxRows; i++ {
		it := mn.items[i]
		arrow := ""
		if it.sub {
			arrow = " →"
		}
		label := ansi.Truncate(it.label, inner-6, "…")
		pad := max(1, inner-3-lipgloss.Width(label)-lipgloss.Width(arrow))
		line := " " + accentStyle.Render(it.icon) + " " + label + strings.Repeat(" ", pad) + subtleStyle.Render(arrow)
		if i == mn.cursor {
			line = cursorStyle.Width(inner).Render(line)
		}
		lines = append(lines, line)
	}
	hint := "→ choose · esc close"
	if mn.parent != nil {
		hint = "→ choose · ← back · esc close"
	}
	lines = append(lines, "", subtleStyle.Render(hint))

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
