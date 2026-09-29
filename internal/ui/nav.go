package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

type pageKind int

const (
	pageHome pageKind = iota
	pagePlaylists
	pageAlbums
	pageArtists
	pageTracks
)

// page is one level of the browsing stack.
type page struct {
	kind  pageKind
	title string // breadcrumb label

	// uri is the context a track page lists; empty lists whatever plays now.
	uri string
	// folder is the folder path a playlists page shows.
	folder []string

	cursor, offset int
	filter         string
}

// nowPlaying reports whether the page lists whatever plays now.
func (p page) nowPlaying() bool { return p.kind == pageTracks && p.uri == "" }

type rowKind int

const (
	rowLink  rowKind = iota // opens a page
	rowTrack                // a track of the page's context
)

// row is one line of a page.
type row struct {
	kind  rowKind
	icon  string // one glyph
	label string
	info  string // right aligned
	// uri is what Enter plays: a context for links, the track for tracks.
	uri string
	// open is the page → opens; nil for rows that open a menu instead.
	open *page

	track *api.ContextTrackItem // rowTrack only
	index int                   // rowTrack: position in the context
	count int                   // folder rows: playlists inside
}

func (m Model) page() *page { return &m.stack[len(m.stack)-1] }

// rows lists the page's rows, narrowed by its filter.
func (m Model) rows() []row {
	p := m.page()
	all := m.rowsOf(p)
	if p.filter == "" {
		return all
	}
	words := strings.Fields(strings.ToLower(p.filter))
	var out []row
	for _, r := range all {
		hay := strings.ToLower(r.label)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) }) {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) rowsOf(p *page) []row {
	switch p.kind {
	case pageHome:
		return m.homeRows()
	case pagePlaylists:
		return m.playlistRows(p.folder)
	case pageAlbums:
		return m.albumRows()
	case pageArtists:
		return m.artistRows()
	case pageTracks:
		return m.trackRows()
	}
	return nil
}

func countInfo(s loadState, n int, what string) string {
	switch {
	case errors.Is(s.err, api.ErrNoSession):
		return "…" // loads again once the daemon has logged in
	case s.err != nil:
		return "unavailable"
	case !s.loaded:
		return "…"
	}
	return plural(n, what)
}

// plural formats a count with its noun, singular for one: "1 track".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + strings.TrimSuffix(noun, "s")
	}
	return fmt.Sprintf("%d %s", n, noun)
}

func (m Model) homeRows() []row {
	lib := m.library
	var rows []row
	if uri := m.contextURI(); uri != "" || m.currentURI() != "" {
		label := "Now playing"
		if name := m.contextName(); name != "" {
			label += " · " + name
		}
		rows = append(rows, row{icon: "♪", label: label, uri: uri, open: &page{kind: pageTracks, title: "Now playing"}})
	}
	if u := m.username(); u != "" {
		uri := likedSongsURI(u)
		rows = append(rows, row{icon: "♥", label: likedSongsName, uri: uri, open: &page{kind: pageTracks, title: likedSongsName, uri: uri}})
	}
	rows = append(rows,
		row{icon: "≡", label: "Playlists", info: countInfo(lib.playlistsState, len(lib.playlists), "playlists"), open: &page{kind: pagePlaylists, title: "Playlists"}},
		row{icon: "◎", label: "Albums", info: countInfo(lib.albumsState, len(lib.albums), "albums"), open: &page{kind: pageAlbums, title: "Albums"}},
		row{icon: "☺", label: "Artists", info: countInfo(lib.artistsState, len(lib.artists), "artists"), open: &page{kind: pageArtists, title: "Artists"}},
	)
	return rows
}

// playlistRows lists the folders and playlists directly inside folder, in
// library order; a folder appears where its first playlist is.
func (m Model) playlistRows(folder []string) []row {
	var rows []row
	seen := map[string]int{}
	for _, p := range m.library.playlists {
		if len(p.Folder) < len(folder) || !slices.Equal(p.Folder[:len(folder)], folder) {
			continue
		}
		if len(p.Folder) == len(folder) {
			rows = append(rows, row{icon: "≡", label: p.Name, info: plural(p.Length, "tracks"), uri: p.URI,
				open: &page{kind: pageTracks, title: p.Name, uri: p.URI}})
			continue
		}
		name := p.Folder[len(folder)]
		i, ok := seen[name]
		if !ok {
			i = len(rows)
			seen[name] = i
			path := append(slices.Clone(folder), name)
			rows = append(rows, row{icon: "▸", label: name, open: &page{kind: pagePlaylists, title: name, folder: path}})
		}
		rows[i].count++
		rows[i].info = plural(rows[i].count, "playlists")
	}
	return rows
}

func (m Model) albumRows() []row {
	rows := make([]row, 0, len(m.library.albums))
	for _, a := range m.library.albums {
		label := orURI(a.Name, a.URI)
		if len(a.ArtistNames) > 0 {
			label += " — " + strings.Join(a.ArtistNames, ", ")
		}
		info := ""
		if a.Year > 0 {
			info = fmt.Sprint(a.Year)
		}
		rows = append(rows, row{icon: "◎", label: label, info: info, uri: a.URI,
			open: &page{kind: pageTracks, title: orURI(a.Name, a.URI), uri: a.URI}})
	}
	return rows
}

func (m Model) artistRows() []row {
	rows := make([]row, 0, len(m.library.artists))
	for _, a := range m.library.artists {
		rows = append(rows, row{icon: "☺", label: orURI(a.Name, a.URI), uri: a.URI,
			open: &page{kind: pageTracks, title: orURI(a.Name, a.URI), uri: a.URI}})
	}
	return rows
}

func orURI(name, uri string) string {
	if name != "" {
		return name
	}
	return uri
}

// trackRows lists the tracks of the top page's context.
func (m Model) trackRows() []row {
	ls := m.lists[m.listURI()]
	if ls == nil || ls.ct == nil {
		return nil
	}
	rows := make([]row, len(ls.ct.Tracks))
	for i := range ls.ct.Tracks {
		it := &ls.ct.Tracks[i]
		rows[i] = row{kind: rowTrack, label: trackTitle(*it), uri: it.URI, track: it, index: i}
	}
	return rows
}

func trackTitle(it api.ContextTrackItem) string {
	if it.Track == nil {
		return it.URI
	}
	return it.Track.Name + " — " + strings.Join(it.Track.ArtistNames, ", ")
}

// selectedRow returns the row under the cursor.
func (m Model) selectedRow() (row, bool) {
	rows := m.rows()
	p := m.page()
	if p.cursor < 0 || p.cursor >= len(rows) {
		return row{}, false
	}
	return rows[p.cursor], true
}

// clampOffset keeps the cursor visible after the page got shorter.
func (m *Model) clampOffset() {
	p := m.page()
	scrollTo(p.cursor, &p.offset, m.listHeight())
}

func (m *Model) clampCursor() {
	p := m.page()
	n := len(m.rows())
	p.cursor = max(0, min(p.cursor, n-1))
}

// push opens a page on top of the stack.
func (m Model) push(p page) (tea.Model, tea.Cmd) {
	m.stack = append(slices.Clone(m.stack), p)
	m.navigated, m.followURI = false, ""
	cmd := m.syncList()
	m.followCursor(true)
	return m, tea.Batch(cmd, m.fetchLiked())
}

// back returns to the page below.
func (m Model) back() (tea.Model, tea.Cmd) {
	if len(m.stack) == 1 {
		return m, nil
	}
	m.stack = slices.Clone(m.stack[:len(m.stack)-1])
	m.navigated, m.followURI = true, ""
	return m, tea.Batch(m.syncList(), m.fetchLiked())
}

func (m Model) goHome() (tea.Model, tea.Cmd) {
	m.stack = slices.Clone(m.stack[:1])
	return m, nil
}

// goNowPlaying shows what plays now, with the cursor on the playing track.
func (m Model) goNowPlaying() (tea.Model, tea.Cmd) {
	if m.page().nowPlaying() {
		m.navigated = false
		m.followCursor(true)
		return m, m.fetchLiked()
	}
	return m.push(page{kind: pageTracks, title: "Now playing"})
}

func (m Model) handlePageKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.page()
	rows := m.rows()
	if navigate(msg.String(), &p.cursor, &p.offset, len(rows), m.listHeight()) {
		if p.kind == pageTracks {
			m.navigated = true
		}
		return m, m.fetchLiked()
	}

	switch msg.String() {
	case "right", "l":
		r, ok := m.selectedRow()
		switch {
		case !ok:
			return m, nil
		case r.open != nil:
			return m.push(*r.open)
		case r.kind == rowTrack:
			return m.openTrackMenu(r)
		}
	case "left", "h", "backspace":
		return m.back()
	case "enter":
		r, ok := m.selectedRow()
		if !ok {
			return m, nil
		}
		return m.playRow(r)
	}
	return m, nil
}

// playRow plays a row: a context from its start, or a track within the
// page's context.
func (m Model) playRow(r row) (tea.Model, tea.Cmd) {
	switch {
	case r.kind == rowTrack:
		ctxURI, trackURI := m.listURI(), r.uri
		m.followURI, m.navigated = trackURI, false // follow the track we start
		return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, ctxURI, trackURI) })
	case r.uri != "" && r.open != nil && r.open.kind == pageTracks && r.open.uri != "":
		uri, name := r.uri, r.label
		m.setNote("Playing " + name)
		return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, uri, "") })
	case r.open != nil:
		return m.push(*r.open)
	}
	return m, nil
}

// enqueueSelected adds the selected track to the queue.
func (m Model) enqueueSelected() (tea.Model, tea.Cmd) {
	r, ok := m.selectedRow()
	if !ok || r.kind != rowTrack {
		m.setErr(errors.New("select a track to queue"))
		return m, nil
	}
	uri := r.uri
	m.setNote("Queued: " + r.label)
	return m, m.action(func(ctx context.Context) error { return m.client.AddToQueue(ctx, uri) })
}

// navigate moves a list cursor for the common movement keys and reports
// whether the key was one of them.
func navigate(key string, cursor, offset *int, n, rows int) bool {
	page := max(1, rows-1)
	switch key {
	case "up", "k":
		*cursor--
	case "down", "j":
		*cursor++
	case "pgup", "ctrl+u":
		*cursor -= page
	case "pgdown", "ctrl+d":
		*cursor += page
	case "home", "g":
		*cursor = 0
	case "end", "G":
		*cursor = n - 1
	default:
		return false
	}
	*cursor = max(0, min(*cursor, n-1))
	scrollTo(*cursor, offset, rows)
	return true
}

// scrollTo moves offset the least needed to keep cursor within rows.
func scrollTo(cursor int, offset *int, rows int) {
	if cursor < *offset {
		*offset = cursor
	}
	if cursor >= *offset+rows {
		*offset = cursor - rows + 1
	}
	*offset = max(0, *offset)
}

// centerOn puts cursor in the middle of the view unless it is already visible.
func centerOn(cursor int, offset *int, rows int) {
	if cursor < *offset || cursor >= *offset+rows {
		*offset = max(0, cursor-rows/2)
	}
}
