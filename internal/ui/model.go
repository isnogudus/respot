// Package ui implements the Bubble Tea terminal UI.
//
// The UI is a stack of pages browsed like lynx: ↑/↓ move, → opens the row
// under the cursor, ← goes back, Enter plays. The player sits on top.
package ui

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const (
	pollInterval     = 2 * time.Second
	tickInterval     = 250 * time.Millisecond
	listPollInterval = time.Second
	// listMaxStalls is how many polls in a row may bring no newly resolved
	// tracks before a list stops polling; a daemon whose cache cannot hold
	// the whole list would otherwise be asked forever.
	listMaxStalls  = 15
	seekStepMs     = 10_000
	errorTTL       = 6 * time.Second
	requestTimeout = 5 * time.Second
)

type (
	// EventMsg carries a daemon event from the WebSocket.
	EventMsg api.Event
	// WSStateMsg reports whether the event WebSocket is connected.
	WSStateMsg bool

	statusMsg struct {
		st  *api.Status
		err error
	}
	actionMsg   struct{ err error }
	tickMsg     time.Time
	pollMsg     time.Time
	listPollMsg struct{ uri string }
	listMsg     struct {
		uri string
		ct  *api.ContextTracks
		err error
	}
)

type inputMode int

const (
	inputNone inputMode = iota
	inputPlay
	inputQueue
	inputFilter
)

// listState is a loaded context track listing.
type listState struct {
	ct     *api.ContextTracks
	err    error
	stalls int // polls in a row without newly resolved tracks
}

// Model is the root UI model.
type Model struct {
	client *api.Client

	status    *api.Status
	statusAt  time.Time
	loaded    bool
	reachable bool
	connErr   error
	wsUp      bool

	err    error
	errAt  time.Time
	note   string
	noteAt time.Time

	width, height int
	showHelp      bool

	// stack holds the pages browsed into; stack[0] is the start page.
	stack []page

	// lists caches context track listings by URI, so going back to a page
	// does not load it again.
	lists map[string]*listState

	followURI string // playing track the cursor was last moved to automatically
	navigated bool   // user moved the cursor on the followed list

	library libraryData

	menu *menu

	covers     covers
	coverMode  CoverMode
	coverOff   bool    // hidden with i
	cellAspect float64 // cell height over width, 0 when unknown

	// Liked state of tracks, asked for the playing track and visible rows.
	liked            map[string]bool
	likedPending     map[string]bool
	likedAt          time.Time
	likedUnavailable bool

	mode  inputMode
	input textinput.Model
}

// Options configure the UI.
type Options struct {
	Cover CoverMode
}

// New creates the root model.
func New(client *api.Client, opts Options) Model {
	ti := textinput.New()
	ti.CharLimit = 256
	return Model{
		client:    client,
		input:     ti,
		stack:     []page{{kind: pageHome, title: "Start"}},
		lists:     map[string]*listState{},
		coverMode: opts.Cover.resolve(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), tick(), poll(), m.fetchPlaylists(), m.fetchAlbums(), m.fetchArtists())
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func poll() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return pollMsg(t) })
}

func (m Model) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		st, err := m.client.Status(ctx)
		return statusMsg{st: st, err: err}
	}
}

// action runs a player command and reports its result.
func (m Model) action(fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return actionMsg{err: fn(ctx)}
	}
}

func (m *Model) setErr(err error) {
	m.err, m.errAt = err, time.Now()
}

func (m *Model) setNote(note string) {
	m.note, m.noteAt = note, time.Now()
}

// position returns the interpolated playback position in milliseconds.
func (m Model) position() int64 {
	st := m.status
	if st == nil || st.Track == nil {
		return 0
	}
	pos := st.Track.Position
	if !st.Paused && !st.Stopped && !st.Buffering {
		pos += time.Since(m.statusAt).Milliseconds()
	}
	return max(0, min(pos, st.Track.Duration))
}

func (m Model) contextURI() string {
	if m.status == nil || m.status.ContextURI == nil {
		return ""
	}
	return *m.status.ContextURI
}

func (m Model) currentURI() string {
	if m.status == nil || m.status.Track == nil {
		return ""
	}
	return m.status.Track.URI
}

func (m Model) username() string {
	if m.status == nil {
		return ""
	}
	return m.status.Username
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, min(msg.Width, maxWidth)-40)
		m.cellAspect = terminalCellAspect()
		m.clampOffset()
		return m, nil

	case tickMsg:
		if m.err != nil && time.Since(m.errAt) > errorTTL {
			m.err = nil
		}
		if m.note != "" && time.Since(m.noteAt) > errorTTL {
			m.note = ""
		}
		return m, tick()

	case pollMsg:
		return m, tea.Batch(m.fetchStatus(), poll())

	case EventMsg:
		return m, m.fetchStatus()

	case actionMsg:
		if msg.err != nil {
			m.setErr(msg.err)
		}
		return m, m.fetchStatus()

	case WSStateMsg:
		m.wsUp = bool(msg)
		return m, nil

	case statusMsg:
		m.loaded = true
		if msg.err != nil {
			m.reachable, m.connErr = false, msg.err
			return m, nil
		}
		m.reachable, m.connErr = true, nil
		m.status, m.statusAt = msg.st, time.Now()
		cmd := m.syncList()
		m.followCursor(false)
		return m, tea.Batch(cmd, m.fetchLiked(), m.fetchCovers())

	case coverMsg:
		return m.applyCover(msg), nil

	case listMsg:
		return m.applyList(msg)

	case listPollMsg:
		if msg.uri == m.listURI() {
			return m, m.fetchList(msg.uri)
		}
		return m, nil

	case playlistsMsg, albumsMsg, artistsMsg:
		m.library.apply(msg)
		m.clampCursor()
		return m, nil

	case likedMsg:
		return m.applyLiked(msg)

	case libraryWriteMsg:
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.setNote(msg.note)
		if msg.likedURI != "" && m.liked != nil {
			m.liked[msg.likedURI] = msg.liked
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case m.mode != inputNone:
			return m.updateInput(msg)
		case m.showHelp:
			m.showHelp = false
			return m, nil
		case m.menu != nil:
			return m.handleMenuKey(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey handles keys on the pages; menus and inputs have their own.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.status
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case " ":
		return m, m.action(m.client.PlayPause)
	case "n":
		return m, m.action(m.client.Next)
	case "p":
		return m, m.action(m.client.Prev)
	case "shift+right":
		return m, m.action(func(ctx context.Context) error { return m.client.Seek(ctx, seekStepMs, true) })
	case "shift+left":
		return m, m.action(func(ctx context.Context) error { return m.client.Seek(ctx, -seekStepMs, true) })
	case "+", "=":
		return m, m.volumeCmd(1)
	case "-", "_":
		return m, m.volumeCmd(-1)
	case "s":
		if st == nil {
			return m, nil
		}
		on := !st.ShuffleContext
		return m, m.action(func(ctx context.Context) error { return m.client.SetShuffle(ctx, on) })
	case "r":
		if st == nil {
			return m, nil
		}
		return m, m.action(m.cycleRepeat(st))
	case "f":
		return m.toggleLiked()
	case "A":
		return m.openAddMenu()
	case "e":
		return m.enqueueSelected()
	case "o":
		return m.startInput(inputPlay)
	case "a":
		return m.startInput(inputQueue)
	case "/":
		return m.startInput(inputFilter)
	case "i":
		m.coverOff = !m.coverOff
		m.clampOffset()
		return m, m.fetchCovers()
	case "m":
		return m.goHome()
	case "c":
		return m.goNowPlaying()
	case "ctrl+r":
		return m, m.reloadLibrary()
	case "esc":
		if p := m.page(); p.filter != "" {
			p.filter, p.cursor, p.offset = "", 0, 0
		}
		return m, nil
	}
	return m.handlePageKey(msg)
}

func (m Model) volumeCmd(dir int) tea.Cmd {
	steps := 100
	if m.status != nil && m.status.VolumeSteps > 0 {
		steps = m.status.VolumeSteps
	}
	delta := dir * max(1, steps/20)
	return m.action(func(ctx context.Context) error { return m.client.SetVolume(ctx, delta, true) })
}

// cycleRepeat advances off → context → track → off.
func (m Model) cycleRepeat(st *api.Status) func(context.Context) error {
	c := m.client
	return func(ctx context.Context) error {
		switch {
		case st.RepeatTrack:
			if err := c.SetRepeatTrack(ctx, false); err != nil {
				return err
			}
			return c.SetRepeatContext(ctx, false)
		case st.RepeatContext:
			return c.SetRepeatTrack(ctx, true)
		default:
			return c.SetRepeatContext(ctx, true)
		}
	}
}

func (m Model) startInput(mode inputMode) (tea.Model, tea.Cmd) {
	m.mode = mode
	m.input.SetValue("")
	m.input.Placeholder = "spotify:… or https://open.spotify.com/…"
	switch mode {
	case inputPlay:
		m.input.Prompt = "Play URI: "
	case inputQueue:
		m.input.Prompt = "Queue URI: "
	case inputFilter:
		m.input.Prompt = "/"
		m.input.Placeholder = "filter this page"
		m.input.SetValue(m.page().filter)
		m.input.CursorEnd()
	}
	return m, m.input.Focus()
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == inputFilter {
		return m.updateFilterInput(msg)
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = inputNone
		m.input.Blur()
		return m, nil
	case "enter":
		mode := m.mode
		m.mode = inputNone
		m.input.Blur()
		uri, err := normalizeURI(m.input.Value())
		if err != nil {
			m.setErr(err)
			return m, nil
		}
		if mode == inputPlay {
			return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, uri, "") })
		}
		m.setNote("Queued: " + uri)
		return m, m.action(func(ctx context.Context) error { return m.client.AddToQueue(ctx, uri) })
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateFilterInput narrows the current page while the filter is typed.
// Enter keeps the filter, esc drops it.
func (m Model) updateFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.page()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = inputNone
		m.input.Blur()
		p.filter, p.cursor, p.offset = "", 0, 0
		return m, nil
	case "enter", "down", "up":
		m.mode = inputNone
		m.input.Blur()
		return m, m.fetchLiked()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if v := m.input.Value(); v != p.filter {
		p.filter, p.cursor, p.offset = v, 0, 0
	}
	return m, cmd
}

// normalizeURI accepts spotify: URIs and open.spotify.com links.
func normalizeURI(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty URI")
	}
	if strings.HasPrefix(s, "spotify:") {
		return s, nil
	}
	u, err := url.Parse(s)
	if err != nil || !strings.HasSuffix(u.Host, "spotify.com") {
		return "", errors.New("not a Spotify URI or link: " + s)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && strings.HasPrefix(parts[0], "intl-") {
		parts = parts[1:]
	}
	if len(parts) < 2 {
		return "", errors.New("cannot parse Spotify link: " + s)
	}
	return "spotify:" + strings.Join(parts, ":"), nil
}
