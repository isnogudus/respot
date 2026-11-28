package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// setOp narrows a track page to the tracks that are, or are not, in another
// listing: "Liked Songs − Cool" keeps the liked tracks Cool lacks. Tracks
// are compared by uri, so the same recording on another album counts as
// another track.
type setOp struct {
	uri  string
	name string
	only bool // keep the tracks in uri; else those not in it
}

func (o setOp) title() string {
	if o.only {
		return "∩ " + o.name
	}
	return "− " + o.name
}

// addAllBatch is how many tracks one request adds to a playlist.
const addAllBatch = 100

// members lists the track uris of a listing, or nil until the daemon has
// enumerated it. Only the uris are needed, not the tracks' metadata.
func (m Model) members(uri string) map[string]bool {
	ls := m.lists[uri]
	if ls == nil || ls.ct == nil || !ls.ct.Ready {
		return nil
	}
	set := make(map[string]bool, len(ls.ct.Tracks))
	for _, it := range ls.ct.Tracks {
		set[it.URI] = true
	}
	return set
}

// applySets narrows rows by the page's set operations; it reports false
// while a listing they compare with is not loaded yet.
func (m Model) applySets(p *page, rows []row) ([]row, bool) {
	for _, op := range p.sets {
		set := m.members(op.uri)
		if set == nil {
			return nil, false
		}
		rows = slices.DeleteFunc(slices.Clone(rows), func(r row) bool { return set[r.uri] != op.only })
	}
	return rows, true
}

// pendingSet names the first listing the page compares with that is not
// loaded yet.
func (m Model) pendingSet(p *page) (setOp, bool) {
	for _, op := range p.sets {
		if m.members(op.uri) == nil {
			return op, true
		}
	}
	return setOp{}, false
}

// pageLists are the listings the top page shows or compares with.
func (m Model) pageLists() []string {
	uri := m.listURI()
	if uri == "" {
		return nil
	}
	out := []string{uri}
	for _, op := range m.page().sets {
		if !slices.Contains(out, op.uri) {
			out = append(out, op.uri)
		}
	}
	return out
}

// listUsable reports whether the top page, or the filtered list playing, has
// what it needs of uri: the listed context needs its tracks' metadata, a
// compared one just its uris.
func (m Model) listUsable(uri string) bool {
	ls := m.lists[uri]
	if ls == nil || ls.ct == nil || ls.stale {
		return false
	}
	if uri == m.listURI() || (m.played != nil && uri == m.played.page.uri) {
		return ls.ct.Complete()
	}
	return ls.ct.Ready
}

// openSetMenu opens F: narrow the track page by a playlist, and act on what
// a narrowed page shows.
func (m Model) openSetMenu() (tea.Model, tea.Cmd) {
	p := m.page()
	uri := m.listURI()
	if p.kind != pageTracks || uri == "" {
		m.setErr(errors.New("open a track list to filter it by a playlist"))
		return m, nil
	}

	mn := &menu{title: "Filter " + p.title + " by a playlist"}
	if len(p.sets) > 0 {
		if rows, ok := m.applySets(p, m.trackRowsAll()); ok {
			uris := addableURIs(rows)
			mn.items = append(mn.items,
				menuItem{icon: "+", label: fmt.Sprintf("Add all %s to a playlist", plural(len(uris), "tracks")), sub: true, run: func(m Model) (Model, tea.Cmd) {
					m.menu = m.addAllMenu(uris)
					return m, nil
				}},
				menuItem{icon: "»", label: fmt.Sprintf("Queue all %s", plural(len(uris), "tracks")), run: func(m Model) (Model, tea.Cmd) {
					return m, m.queueAllCmd(uris)
				}})
		}
	}

	used := map[string]bool{uri: true}
	for _, op := range p.sets {
		used[op.uri] = true
	}
	type source struct{ uri, label, name string }
	var sources []source
	if u := m.username(); u != "" {
		sources = append(sources, source{likedSongsURI(u), likedSongsName, likedSongsName})
	}
	for _, pl := range m.library.playlists {
		label := pl.Name
		if len(pl.Folder) > 0 {
			label = strings.Join(pl.Folder, " › ") + " › " + pl.Name
		}
		sources = append(sources, source{pl.URI, label, pl.Name})
	}
	for _, s := range sources {
		if used[s.uri] {
			continue
		}
		short := s.name
		mn.items = append(mn.items, menuItem{icon: "≡", label: s.label, sub: true, run: func(m Model) (Model, tea.Cmd) {
			pick := func(only bool) menuItem {
				op := setOp{uri: s.uri, name: short, only: only}
				label := "Without the tracks in " + short
				if only {
					label = "Only the tracks also in " + short
				}
				return menuItem{icon: map[bool]string{false: "−", true: "∩"}[only], label: label, run: func(m Model) (Model, tea.Cmd) {
					nm, cmd := m.pushSet(op)
					return nm.(Model), cmd
				}}
			}
			m.menu = &menu{title: p.title + " and " + short, items: []menuItem{pick(false), pick(true)}}
			return m, nil
		}})
	}
	m.menu = mn
	return m, nil
}

// pushSet opens the top page again, narrowed by op.
func (m Model) pushSet(op setOp) (tea.Model, tea.Cmd) {
	p := *m.page()
	next := page{kind: pageTracks, title: op.title(), uri: p.uri, sets: append(slices.Clone(p.sets), op)}
	if p.nowPlaying() {
		next.uri = m.contextURI() // keep listing it when something else plays
	}
	return m.push(next)
}

func addableURIs(rows []row) []string {
	var uris []string
	for _, r := range rows {
		if r.kind == rowTrack && isAddable(r.uri) {
			uris = append(uris, r.uri)
		}
	}
	return uris
}

// addAllMenu lists the playlists the shown tracks can be added to.
func (m Model) addAllMenu(uris []string) *menu {
	mn := &menu{title: "Add " + plural(len(uris), "tracks") + " to …"}
	for _, pl := range m.library.playlists {
		if !pl.CanEdit {
			continue
		}
		label := pl.Name
		if len(pl.Folder) > 0 {
			label = strings.Join(pl.Folder, " › ") + " › " + pl.Name
		}
		mn.items = append(mn.items, menuItem{icon: "≡", label: label, sub: true, run: func(m Model) (Model, tea.Cmd) {
			// Skip what the playlist already holds when its listing is known.
			add := uris
			if held := m.members(pl.URI); held != nil {
				add = slices.DeleteFunc(slices.Clone(uris), func(u string) bool { return held[u] })
			}
			if len(add) == 0 {
				m.setNote(pl.Name + " already holds all of them")
				return m, nil
			}
			subtitle := ""
			if skipped := len(uris) - len(add); skipped > 0 {
				subtitle = plural(skipped, "tracks") + " it already holds are left out."
			}
			m.menu = confirmMenu("Add "+plural(len(add), "tracks")+" to "+pl.Name+"?", subtitle,
				menuItem{icon: "+", label: "Add", run: func(m Model) (Model, tea.Cmd) {
					return m, m.addAllCmd(add, pl.URI, pl.Name)
				}})
			return m, nil
		}})
	}
	return mn
}

func (m Model) addAllCmd(uris []string, playlistURI, name string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		added := 0
		for chunk := range slices.Chunk(uris, addAllBatch) {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			err := c.AddToPlaylist(ctx, playlistURI, chunk)
			cancel()
			if err != nil {
				if added > 0 {
					err = fmt.Errorf("added %d of %d tracks to %s: %w", added, len(uris), name, err)
				}
				return libraryWriteMsg{err: err, changed: playlistURI, playlistsChanged: added > 0}
			}
			added += len(chunk)
		}
		return libraryWriteMsg{note: "Added " + plural(added, "tracks") + " to " + name, changed: playlistURI, playlistsChanged: true}
	}
}

func (m Model) queueAllCmd(uris []string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		for i, uri := range uris {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			err := c.AddToQueue(ctx, uri)
			cancel()
			if err != nil {
				if i > 0 {
					err = fmt.Errorf("queued %d of %d tracks: %w", i, len(uris), err)
				}
				return libraryWriteMsg{err: err}
			}
		}
		return libraryWriteMsg{note: "Queued " + plural(len(uris), "tracks")}
	}
}
