package ui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const (
	maxWidth = 100
	// headerHeight is the player (three lines), the rule and the breadcrumb.
	headerHeight = 5
	// footerHeight is the message line plus the key line.
	footerHeight = 2
)

var (
	green  = lipgloss.Color("#1DB954")
	subtle = lipgloss.AdaptiveColor{Light: "#9A9A9A", Dark: "#5C5C5C"}
	muted  = lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#A0A0A0"}
	red    = lipgloss.Color("#E5534B")

	titleStyle  = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(green)
	boldAccent  = accentStyle.Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	subtleStyle = lipgloss.NewStyle().Foreground(subtle)
	errStyle    = lipgloss.NewStyle().Foreground(red)
	cursorStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#E4F6EA", Dark: "#23352A"})
	keyStyle    = lipgloss.NewStyle().Foreground(green).Bold(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(green).Padding(0, 1)
)

func (m Model) contentWidth() int { return min(m.width, maxWidth) }

// listHeight is how many rows a page shows.
func (m Model) listHeight() int {
	return max(3, m.height-headerHeight-footerHeight)
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	w := m.contentWidth()

	lines := m.renderPlayer(w)
	lines = append(lines, subtleStyle.Render(strings.Repeat("─", w)), m.renderBreadcrumb(w))
	lines = append(lines, m.renderPage(w)...)

	bodyHeight := max(0, m.height-footerHeight)
	if len(lines) > bodyHeight {
		lines = lines[:bodyHeight]
	}
	for len(lines) < bodyHeight {
		lines = append(lines, "")
	}

	var box string
	switch {
	case m.showHelp:
		box = m.renderHelp(w)
	case m.menu != nil:
		box = m.renderMenu()
	}
	if box != "" {
		x := max(0, (w-lipgloss.Width(box))/2)
		y := max(0, (bodyHeight-lipgloss.Height(box))/2)
		lines = overlay(lines, box, x, y)
	}

	return strings.Join(lines, "\n") + "\n" + m.renderMessageLine() + "\n" + m.renderKeyLine()
}

// renderPlayer is the three player lines: device, track, progress.
func (m Model) renderPlayer(w int) []string {
	left := boldAccent.Render(" ♫ go-librespot")
	if st := m.status; st != nil {
		left += mutedStyle.Render(fmt.Sprintf("  %s · %s", st.DeviceName, strings.ToLower(st.DeviceType)))
	}
	var right string
	switch {
	case !m.reachable:
		right = errStyle.Render("● offline ")
	case m.wsUp:
		right = accentStyle.Render("● live ")
	default:
		right = subtleStyle.Render("● polling ")
	}
	device := left + strings.Repeat(" ", max(1, w-lipgloss.Width(left)-lipgloss.Width(right))) + right

	switch {
	case !m.loaded:
		return []string{device, mutedStyle.Render(" Connecting to " + m.client.Base() + " …"), ""}
	case !m.reachable:
		return []string{device, errStyle.Render(ansi.Truncate(" Cannot reach go-librespot: "+fmt.Sprint(m.connErr), w, "…")),
			subtleStyle.Render(" Retrying every few seconds.")}
	case m.status == nil:
		return []string{device, mutedStyle.Render(" No active Spotify session."),
			subtleStyle.Render(" Select this device in a Spotify app to start.")}
	case m.status.Track == nil:
		return []string{device, mutedStyle.Render(" Nothing playing."), m.renderControls(w, "")}
	}

	st, t := m.status, m.status.Track
	icon := "▶"
	switch {
	case st.Buffering:
		icon = "…"
	case st.Stopped:
		icon = "■"
	case st.Paused:
		icon = "⏸"
	}
	title := " " + accentStyle.Render(icon) + " " + titleStyle.Render(t.Name)
	if liked, _ := m.likedState(t.URI); liked {
		title += accentStyle.Render(" ♥")
	}
	title += mutedStyle.Render(" — " + strings.Join(t.ArtistNames, ", "))
	if album := albumLine(t); album != "" {
		title += subtleStyle.Render(" · " + album)
	}

	pos := m.position()
	times := fmt.Sprintf("%s / %s", fmtDur(pos), fmtDur(t.Duration))
	frac := 0.0
	if t.Duration > 0 {
		frac = float64(pos) / float64(t.Duration)
	}
	progress := "   " + bar(frac, 24) + " " + mutedStyle.Render(times)
	return []string{device, ansi.Truncate(title, w, "…"), m.renderControls(w, progress)}
}

var yearRe = regexp.MustCompile(`year:(\d{4})`)

func albumLine(t *api.Track) string {
	album := t.AlbumName
	if y := yearRe.FindStringSubmatch(t.ReleaseDate); y != nil {
		album += " (" + y[1] + ")"
	}
	return album
}

// renderControls is the progress line with volume, shuffle and repeat.
func (m Model) renderControls(w int, progress string) string {
	st := m.status
	if st == nil {
		return progress
	}
	steps := max(1, st.VolumeSteps)
	parts := []string{mutedStyle.Render(fmt.Sprintf("vol %d%%", st.Volume*100/steps))}
	if st.ShuffleContext {
		parts = append(parts, accentStyle.Render("⤮ shuffle"))
	}
	switch {
	case st.RepeatTrack:
		parts = append(parts, accentStyle.Render("⟳ track"))
	case st.RepeatContext:
		parts = append(parts, accentStyle.Render("⟳ all"))
	}
	if st.Track != nil {
		if q := audioInfo(st.Track); q != "" {
			parts = append(parts, subtleStyle.Render(q))
		}
	}
	right := strings.Join(parts, "   ") + " "
	pad := w - lipgloss.Width(progress) - lipgloss.Width(right)
	if pad < 2 {
		return ansi.Truncate(progress, w, "…")
	}
	return progress + strings.Repeat(" ", pad) + right
}

func audioInfo(t *api.Track) string {
	var parts []string
	if t.Codec != "" && t.Codec != "unknown" {
		parts = append(parts, t.Codec)
	}
	if t.Bitrate != nil {
		parts = append(parts, fmt.Sprintf("%d kbps", *t.Bitrate))
	}
	return strings.Join(parts, " ")
}

// renderBreadcrumb shows where the page sits, its size and filter.
func (m Model) renderBreadcrumb(w int) string {
	var crumbs []string
	for _, p := range m.stack {
		title := p.title
		if p.nowPlaying() {
			if name := m.contextName(); name != "" {
				title += " · " + name
			}
		}
		crumbs = append(crumbs, title)
	}
	last := len(crumbs) - 1
	left := " " + subtleStyle.Render(strings.Join(crumbs[:last], " › "))
	if last > 0 {
		left += subtleStyle.Render(" › ")
	}
	left += titleStyle.Render(crumbs[last])

	p := m.page()
	info := ""
	if n := len(m.rowsOf(p)); n > 0 {
		info = fmt.Sprint(n)
		if p.filter != "" {
			info = fmt.Sprintf("%d of %d", len(m.rows()), n)
		}
	}
	if p.filter != "" {
		info += accentStyle.Render("  /" + p.filter)
	}
	right := subtleStyle.Render(info) + " "
	pad := w - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 2 {
		return ansi.Truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", pad) + right
}

// renderPage is the visible rows of the page, or why there are none.
func (m Model) renderPage(w int) []string {
	p := *m.page()
	rows := m.rows()
	if len(rows) == 0 {
		return []string{subtleStyle.Render(" " + m.emptyText())}
	}

	height := m.listHeight()
	scrollTo(p.cursor, &p.offset, height)
	playingCtx, playingTrack := m.contextURI(), m.currentURI()
	numW := len(fmt.Sprint(len(m.rowsOf(m.page()))))

	var out []string
	for i := p.offset; i < len(rows) && i < p.offset+height; i++ {
		r := rows[i]
		mark := "  "
		playing := (r.kind == rowTrack && r.uri == playingTrack && m.listURI() == playingCtx) ||
			(r.kind == rowLink && r.uri != "" && r.uri == playingCtx)
		if playing {
			mark = accentStyle.Render("▶ ")
		}

		var lead, info string
		switch r.kind {
		case rowTrack:
			lead = subtleStyle.Render(fmt.Sprintf("%*d ", numW, r.index+1))
			heart := "  "
			if liked, _ := m.likedState(r.uri); liked {
				heart = accentStyle.Render("♥ ")
			}
			dur := ""
			if r.track != nil && r.track.Track != nil {
				dur = fmtDur(r.track.Track.Duration)
			}
			info = heart + mutedStyle.Render(dur)
		default:
			lead = accentStyle.Render(r.icon) + " "
			info = mutedStyle.Render(r.info) + subtleStyle.Render(" ›")
		}

		label := r.label
		if playing {
			label = accentStyle.Render(label)
		}
		prefix := " " + mark + lead
		avail := w - lipgloss.Width(prefix) - lipgloss.Width(info) - 2
		label = ansi.Truncate(label, max(5, avail), "…")
		pad := max(1, w-1-lipgloss.Width(prefix)-lipgloss.Width(label)-lipgloss.Width(info))
		line := prefix + label + strings.Repeat(" ", pad) + info + " "
		if i == p.cursor {
			line = cursorStyle.Width(w).Render(line)
		}
		out = append(out, line)
	}
	return out
}

// emptyText explains an empty page.
func (m Model) emptyText() string {
	p := m.page()
	if p.filter != "" {
		return "Nothing matches /" + p.filter + " — esc clears the filter."
	}
	lib := m.library
	state := func(s loadState, what string) string {
		switch {
		case s.err != nil:
			return libraryErrText(s.err)
		case !s.loaded:
			return "Loading " + what + " …"
		}
		return "No " + what + " in your library."
	}
	switch p.kind {
	case pagePlaylists:
		return state(lib.playlistsState, "playlists")
	case pageAlbums:
		return state(lib.albumsState, "albums")
	case pageArtists:
		return state(lib.artistsState, "artists")
	case pageTracks:
		uri := m.listURI()
		ls := m.lists[uri]
		switch {
		case uri == "":
			return "Nothing is playing from a playlist, album or Liked Songs."
		case ls == nil || (ls.err == nil && (ls.ct == nil || !ls.ct.Ready)):
			return "Loading tracks …"
		case errors.Is(ls.err, api.ErrContextTracksUnavailable):
			return "Track listing needs `metadata: { enabled: true }` in the daemon's config.yml."
		case ls.err != nil:
			return ls.err.Error()
		}
		return "No tracks."
	}
	return ""
}

// renderMessageLine shows the input prompt, the last error or a note; it is
// blank otherwise.
func (m Model) renderMessageLine() string {
	w := m.contentWidth()
	switch {
	case m.mode != inputNone:
		return " " + m.input.View() + subtleStyle.Render("   enter ok · esc cancel")
	case m.err != nil:
		return ansi.Truncate(errStyle.Render(" ✗ "+m.err.Error()), w, "…")
	case m.note != "":
		return ansi.Truncate(accentStyle.Render(" ✓ "+m.note), w, "…")
	}
	return ""
}

// renderKeyLine is the always-visible line of the essential keys.
func (m Model) renderKeyLine() string {
	keys := [][2]string{{"↑↓", "move"}, {"→", "open"}, {"←", "back"}, {"⏎", "play"}, {"space", "pause"},
		{"⇧←→", "seek"}, {"/", "filter"}, {"?", "help"}, {"q", "quit"}}
	var parts []string
	for _, k := range keys {
		parts = append(parts, keyStyle.Render(k[0])+" "+subtleStyle.Render(k[1]))
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), m.contentWidth(), "…")
}

func (m Model) renderHelp(w int) string {
	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"Browse", [][2]string{
			{"↑ ↓  j k", "move (pgup pgdn g G)"},
			{"→  l", "open · on a track: actions"},
			{"←  h", "back"},
			{"enter", "play the row"},
			{"/", "filter this page (esc clears)"},
			{"m", "start page"},
			{"c", "now playing, on the playing track"},
			{"ctrl+r", "reload the library"},
		}},
		{"Player", [][2]string{
			{"space", "play / pause"},
			{"n  p", "next / previous track"},
			{"⇧← ⇧→", "seek ±10 s"},
			{"+  -", "volume ±5 %"},
			{"s  r", "shuffle · repeat off/all/track"},
		}},
		{"Tracks", [][2]string{
			{"f", "add to / remove from Liked Songs"},
			{"A", "add to Liked Songs or a playlist"},
			{"e", "add selected track to the queue"},
			{"o  a", "play / queue a Spotify URI or link"},
		}},
	}
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(titleStyle.Render(s.title) + "\n")
		for _, r := range s.rows {
			b.WriteString(keyStyle.Render(fmt.Sprintf("%-10s", r[0])) + mutedStyle.Render(r[1]) + "\n")
		}
	}
	b.WriteString("\n" + subtleStyle.Render("any key closes"))
	return boxStyle.Width(min(56, w-4)).Render(b.String())
}

func bar(frac float64, width int) string {
	frac = max(0, min(frac, 1))
	filled := int(frac*float64(width) + 0.5)
	return accentStyle.Render(strings.Repeat("━", filled)) + subtleStyle.Render(strings.Repeat("─", width-filled))
}

func fmtDur(ms int64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
