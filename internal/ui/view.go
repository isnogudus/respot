package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const maxWidth = 100

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
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle).Padding(0, 1)
	cursorStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#E4F6EA", Dark: "#23352A"})
	keyStyle    = lipgloss.NewStyle().Foreground(green).Bold(true)
)

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	top := m.renderTop()
	parts := []string{top}
	if m.showList {
		parts = append(parts, m.renderList(m.listHeightFor(top)))
	}
	view := lipgloss.JoinVertical(lipgloss.Left, parts...)

	footer := m.renderFooter()
	gap := m.height - lipgloss.Height(view) - lipgloss.Height(footer)
	if gap > 0 {
		view += strings.Repeat("\n", gap)
	}
	return view + "\n" + footer
}

func (m Model) contentWidth() int { return min(m.width, maxWidth) }

func (m Model) renderTop() string {
	w := m.contentWidth()
	inner := w - 4 // border + padding

	var body string
	switch {
	case !m.loaded:
		body = mutedStyle.Render("Connecting to " + m.client.Base() + " …")
	case !m.reachable:
		body = errStyle.Render("Cannot reach go-librespot at "+m.client.Base()) + "\n" +
			mutedStyle.Render(ansi.Truncate(fmt.Sprint(m.connErr), inner, "…")) + "\n" +
			subtleStyle.Render("Retrying every few seconds.")
	case m.status == nil:
		body = mutedStyle.Render("No active Spotify session.") + "\n" +
			subtleStyle.Render("Select this device in a Spotify app to start.")
	case m.status.Track == nil:
		body = mutedStyle.Render("Nothing playing.") + "\n" +
			subtleStyle.Render("Press o to play a Spotify URI.")
	default:
		body = m.renderNowPlaying(inner)
	}
	if m.status != nil {
		body += "\n\n" + m.renderControls(inner)
	}

	lines := []string{m.renderHeader(w), boxStyle.Width(w - 2).Render(body)}
	if m.status != nil && m.status.NextTrack != nil && !m.showList {
		nt := m.status.NextTrack
		lines = append(lines, ansi.Truncate(
			" "+subtleStyle.Render("Up next  ")+nt.Name+mutedStyle.Render(" — "+strings.Join(nt.ArtistNames, ", ")),
			w, "…"))
	}
	if m.showHelp {
		lines = append(lines, m.renderHelp(w))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderHeader(w int) string {
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
	pad := max(1, w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", pad) + right
}

var yearRe = regexp.MustCompile(`year:(\d{4})`)

func (m Model) renderNowPlaying(inner int) string {
	st, t := m.status, m.status.Track
	trunc := func(s string) string { return ansi.Truncate(s, inner, "…") }

	var b strings.Builder
	b.WriteString(trunc(titleStyle.Render(t.Name)) + "\n")
	b.WriteString(trunc(accentStyle.Render(strings.Join(t.ArtistNames, ", "))) + "\n")

	album := t.AlbumName
	if y := yearRe.FindStringSubmatch(t.ReleaseDate); y != nil {
		album += " · " + y[1]
	}
	b.WriteString(trunc(mutedStyle.Render(album)) + "\n")
	if st.ContextName != nil && *st.ContextName != "" {
		b.WriteString(trunc(subtleStyle.Render("from ") + mutedStyle.Render(*st.ContextName)))
	}
	b.WriteString("\n\n")

	pos := m.position()
	icon := "▶"
	switch {
	case st.Buffering:
		icon = "…"
	case st.Stopped:
		icon = "■"
	case st.Paused:
		icon = "⏸"
	}
	left := fmt.Sprintf("%s %s ", icon, fmtDur(pos))
	right := " " + fmtDur(t.Duration)
	barW := max(5, inner-lipgloss.Width(left)-lipgloss.Width(right))
	frac := 0.0
	if t.Duration > 0 {
		frac = float64(pos) / float64(t.Duration)
	}
	b.WriteString(accentStyle.Render(left) + bar(frac, barW) + mutedStyle.Render(right))
	return b.String()
}

func (m Model) renderControls(inner int) string {
	st := m.status
	steps := max(1, st.VolumeSteps)
	pct := st.Volume * 100 / steps
	vol := mutedStyle.Render("vol ") + bar(float64(st.Volume)/float64(steps), 12) + mutedStyle.Render(fmt.Sprintf(" %3d%%", pct))

	shuffle := subtleStyle.Render("⤮ shuffle")
	if st.ShuffleContext {
		shuffle = accentStyle.Render("⤮ shuffle")
	}
	repeat := subtleStyle.Render("⟳ repeat")
	switch {
	case st.RepeatTrack:
		repeat = accentStyle.Render("⟳ repeat track")
	case st.RepeatContext:
		repeat = accentStyle.Render("⟳ repeat all")
	}
	line := vol + "   " + shuffle + "   " + repeat

	if t := st.Track; t != nil {
		if q := audioInfo(t); q != "" && lipgloss.Width(line)+3+lipgloss.Width(q) <= inner {
			pad := inner - lipgloss.Width(line) - lipgloss.Width(q)
			line += strings.Repeat(" ", pad) + subtleStyle.Render(q)
		}
	}
	return line
}

func audioInfo(t *api.Track) string {
	var parts []string
	if t.Codec != "" && t.Codec != "unknown" {
		parts = append(parts, t.Codec)
	}
	if t.Bitrate != nil {
		parts = append(parts, fmt.Sprintf("%d kbps", *t.Bitrate))
	}
	if t.SampleRate != nil {
		parts = append(parts, fmt.Sprintf("%.1f kHz", float64(*t.SampleRate)/1000))
	}
	if t.BitDepth != nil {
		parts = append(parts, fmt.Sprintf("%d bit", *t.BitDepth))
	}
	return strings.Join(parts, " · ")
}

// listHeight returns how many rows the track list may use.
func (m Model) listHeight() int { return m.listHeightFor(m.renderTop()) }

func (m Model) listHeightFor(top string) int {
	return max(3, m.height-lipgloss.Height(top)-lipgloss.Height(m.renderFooter())-2)
}

func (m *Model) clampOffset(rows int) {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, m.offset)
}

func (m Model) renderList(rows int) string {
	w := m.contentWidth()
	header := " " + titleStyle.Render("Tracks")
	if m.status != nil && m.status.ContextName != nil {
		header += mutedStyle.Render(" · " + *m.status.ContextName)
	}

	var body []string
	switch {
	case m.listURI == "":
		body = []string{subtleStyle.Render(" Nothing is playing from a playlist or album.")}
	case m.listErr != nil:
		msg := m.listErr.Error()
		if m.listErr == api.ErrContextTracksUnavailable {
			msg = "Track listing is not available. It needs a go-librespot version with\n" +
				" /context/tracks and `metadata: { enabled: true }` in config.yml."
		}
		body = []string{errStyle.Render(" " + msg)}
	case m.list == nil || !m.list.Ready:
		body = []string{subtleStyle.Render(" Loading track list …")}
	default:
		if !m.list.Complete() {
			header += subtleStyle.Render(fmt.Sprintf("  (resolving %d/%d)", m.list.Cached, m.list.Length))
		}
		body = m.renderRows(rows, w)
	}
	return header + "\n" + strings.Join(body, "\n")
}

func (m Model) renderRows(rows, w int) []string {
	m.clampOffset(rows)
	cur := m.currentURI()
	tracks := m.list.Tracks
	numW := len(fmt.Sprint(len(tracks)))

	var out []string
	for i := m.offset; i < len(tracks) && i < m.offset+rows; i++ {
		it := tracks[i]
		mark := "  "
		if it.URI == cur {
			mark = accentStyle.Render("▶ ")
		}
		num := subtleStyle.Render(fmt.Sprintf("%*d ", numW, i+1))
		dur := ""
		if it.Track != nil {
			dur = fmtDur(it.Track.Duration)
		}
		title := trackTitle(it)
		if it.URI == cur {
			title = accentStyle.Render(title)
		}
		avail := w - 2 - lipgloss.Width(mark) - lipgloss.Width(num) - len(dur) - 2
		title = ansi.Truncate(title, max(5, avail), "…")
		pad := max(1, w-2-lipgloss.Width(mark)-lipgloss.Width(num)-lipgloss.Width(title)-len(dur))
		row := " " + mark + num + title + strings.Repeat(" ", pad) + mutedStyle.Render(dur) + " "
		if i == m.cursor {
			row = cursorStyle.Width(w).Render(row)
		}
		out = append(out, row)
	}
	return out
}

func trackTitle(it api.ContextTrackItem) string {
	if it.Track == nil {
		return it.URI
	}
	return it.Track.Name + " — " + strings.Join(it.Track.ArtistNames, ", ")
}

func (m Model) renderFooter() string {
	w := m.contentWidth()
	switch {
	case m.mode != inputNone:
		return " " + m.input.View() + subtleStyle.Render("   enter ok · esc cancel")
	case m.err != nil:
		return ansi.Truncate(errStyle.Render(" ✗ "+m.err.Error()), w, "…")
	case m.note != "":
		return ansi.Truncate(accentStyle.Render(" ✓ "+m.note), w, "…")
	}
	keys := [][2]string{{"space", "play/pause"}, {"n/p", "next/prev"}, {"←/→", "seek"}, {"+/-", "vol"}, {"l", "tracks"}, {"?", "help"}, {"q", "quit"}}
	var parts []string
	for _, k := range keys {
		parts = append(parts, keyStyle.Render(k[0])+" "+subtleStyle.Render(k[1]))
	}
	return ansi.Truncate(" "+strings.Join(parts, "  "), w, "…")
}

func (m Model) renderHelp(w int) string {
	rows := [][2]string{
		{"space", "play / pause"},
		{"n  p", "next / previous track"},
		{"← →", "seek ±10 s"},
		{"+  -", "volume ±5 %"},
		{"s", "toggle shuffle"},
		{"r", "cycle repeat: off → all → track"},
		{"o", "play a Spotify URI or link"},
		{"a", "add a URI or link to the queue"},
		{"l / tab", "toggle track list of current context"},
		{"j k g G", "move in track list (pgup/pgdn too)"},
		{"enter  e", "play / enqueue selected track"},
		{"c", "jump to current track, follow it again"},
		{"q", "quit"},
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(keyStyle.Render(fmt.Sprintf("%-10s", r[0])) + mutedStyle.Render(r[1]) + "\n")
	}
	return boxStyle.Width(w - 2).Render(strings.TrimRight(b.String(), "\n"))
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
