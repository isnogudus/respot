// Package ui implements the Bubble Tea terminal UI.
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
	seekStepMs       = 10_000
	errorTTL         = 6 * time.Second
	requestTimeout   = 5 * time.Second
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
	libraryMsg struct {
		playlists []api.LibraryPlaylist
		err       error
	}
)

// pane is the panel shown below the now-playing box.
type pane int

const (
	paneNone pane = iota
	paneLibrary
	paneTracks
)

type inputMode int

const (
	inputNone inputMode = iota
	inputPlay
	inputQueue
	inputFilter
)

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

	pane pane

	// Track list pane. It shows the playing context, or the context picked
	// in the library while browseURI is set.
	browseURI  string
	browseName string
	listURI    string
	list       *api.ContextTracks
	listErr    error
	cursor     int
	offset     int
	followURI  string // playing track the cursor was last moved to automatically
	navigated  bool   // user moved the cursor since the list was loaded

	// Library pane.
	library    []api.LibraryPlaylist
	libLoaded  bool
	libLoading bool
	libErr     error
	libCursor  int
	libOffset  int
	libFilter  string

	popup *addPopup

	mode  inputMode
	input textinput.Model
}

// New creates the root model.
func New(client *api.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "spotify:… or https://open.spotify.com/…"
	ti.CharLimit = 256
	return Model{client: client, input: ti}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), tick(), poll())
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

func (m Model) fetchList(uri string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ct, err := m.client.ContextTracks(ctx, uri)
		return listMsg{uri: uri, ct: ct, err: err}
	}
}

func (m Model) fetchLibrary() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		pl, err := m.client.LibraryPlaylists(ctx)
		return libraryMsg{playlists: pl, err: err}
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, min(msg.Width, maxWidth)-40)
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

	case EventMsg, actionMsg:
		if a, ok := msg.(actionMsg); ok && a.err != nil {
			m.setErr(a.err)
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
		cmd := m.syncList(false)
		m.followCursor(false)
		return m, cmd

	case listMsg:
		if msg.uri != m.listURI {
			return m, nil
		}
		m.list, m.listErr = msg.ct, msg.err
		if msg.ct != nil {
			m.followCursor(false)
			if !msg.ct.Complete() && m.pane == paneTracks {
				uri := msg.uri
				return m, tea.Tick(listPollInterval, func(time.Time) tea.Msg { return listPollMsg{uri: uri} })
			}
		}
		return m, nil

	case listPollMsg:
		if m.pane == paneTracks && msg.uri == m.listURI {
			return m, m.fetchList(msg.uri)
		}
		return m, nil

	case libraryMsg:
		m.libLoading = false
		m.libErr = msg.err
		if msg.err == nil {
			m.library, m.libLoaded = msg.playlists, true
			m.libCursor = max(0, min(m.libCursor, len(m.libraryEntries())-1))
		}
		return m, nil

	case libraryWriteMsg:
		if msg.err != nil {
			m.setErr(msg.err)
		} else {
			m.note, m.noteAt = msg.note, time.Now()
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode != inputNone {
			return m.updateInput(msg)
		}
		if m.popup != nil {
			return m.handlePopupKey(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

// tracksURI is the context the track list pane shows.
func (m Model) tracksURI() string {
	if m.browseURI != "" {
		return m.browseURI
	}
	return m.contextURI()
}

// syncList (re)loads the track list when it is visible and its context changed.
func (m *Model) syncList(force bool) tea.Cmd {
	if m.pane != paneTracks {
		return nil
	}
	uri := m.tracksURI()
	if uri == m.listURI && !force {
		return nil
	}
	m.listURI, m.list, m.listErr = uri, nil, nil
	m.cursor, m.offset, m.followURI, m.navigated = 0, 0, "", false
	if uri == "" {
		return nil
	}
	return m.fetchList(uri)
}

// followCursor keeps the cursor on the playing track. It stops following once
// the user moves the cursor away and resumes when force is set or the cursor
// is back on the followed track.
func (m *Model) followCursor(force bool) {
	if m.list == nil || m.cursor >= len(m.list.Tracks) || m.listURI != m.contextURI() {
		return
	}
	onFollowed := m.followURI != "" && m.list.Tracks[m.cursor].URI == m.followURI
	if !force && !onFollowed && (m.followURI != "" || m.navigated) {
		return
	}
	cur := m.currentURI()
	for i, it := range m.list.Tracks {
		if it.URI != cur {
			continue
		}
		m.cursor, m.followURI = i, cur
		centerOn(m.cursor, &m.offset, m.listHeight())
		return
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.status
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case " ":
		return m, m.action(m.client.PlayPause)
	case "n", ">":
		return m, m.action(m.client.Next)
	case "p", "<":
		return m, m.action(m.client.Prev)
	case "right", "L":
		return m, m.action(func(ctx context.Context) error { return m.client.Seek(ctx, seekStepMs, true) })
	case "left", "H":
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
	case "A":
		return m.openAddPopup()
	case "f":
		return m.likeTarget()
	case "o":
		return m.startInput(inputPlay)
	case "a":
		return m.startInput(inputQueue)
	case "b":
		if m.pane == paneLibrary {
			m.pane = paneNone
			return m, nil
		}
		return m.openLibrary()
	case "l":
		switch m.pane {
		case paneLibrary:
			return m.browseSelected()
		case paneTracks:
			m.pane, m.browseURI, m.browseName = paneNone, "", ""
			return m, nil
		}
		m.pane, m.browseURI, m.browseName = paneTracks, "", ""
		return m, m.syncList(true)
	case "esc":
		if m.pane == paneTracks && m.browseURI != "" {
			m.pane, m.browseURI, m.browseName = paneLibrary, "", ""
			return m, nil
		}
		if m.pane == paneLibrary {
			return m.handleLibraryKey(msg) // clears the filter first
		}
		m.pane = paneNone
		return m, nil
	}

	switch m.pane {
	case paneTracks:
		return m.handleListKey(msg)
	case paneLibrary:
		return m.handleLibraryKey(msg)
	}
	return m, nil
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

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.list == nil || len(m.list.Tracks) == 0 {
		return m, nil
	}
	if navigate(msg.String(), &m.cursor, &m.offset, len(m.list.Tracks), m.listHeight()) {
		m.navigated = true
		return m, nil
	}
	switch msg.String() {
	case "c":
		m.followCursor(true)
		return m, nil
	case "enter":
		ctxURI, trackURI := m.listURI, m.list.Tracks[m.cursor].URI
		m.followURI = trackURI // follow the track we are about to start
		return m, m.action(func(ctx context.Context) error { return m.client.Play(ctx, ctxURI, trackURI) })
	case "e":
		it := m.list.Tracks[m.cursor]
		m.note, m.noteAt = "Queued: "+trackTitle(it), time.Now()
		return m, m.action(func(ctx context.Context) error { return m.client.AddToQueue(ctx, it.URI) })
	}
	return m, nil
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
		m.input.Placeholder = "filter playlists"
		m.input.SetValue(m.libFilter)
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
		m.note, m.noteAt = "Queued: "+uri, time.Now()
		return m, m.action(func(ctx context.Context) error { return m.client.AddToQueue(ctx, uri) })
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
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
