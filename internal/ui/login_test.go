package ui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/isnogudus/respot/internal/api"
)

// loggingInDaemon answers like a daemon that is still logging in: / is not
// ready, /status has no session and /auth/code has the given body (none when
// empty).
func loggingInDaemon(t *testing.T, authBody string) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`{"playback_ready":false}`))
		case "/auth/code":
			if authBody == "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = w.Write([]byte(authBody))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL)
}

// playerText runs one status round against client and returns the player.
func playerText(t *testing.T, client *api.Client) string {
	t.Helper()
	m := New(client, Options{Cover: CoverOff})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	nm, cmd := m.Update(m.fetchStatus()())
	m = nm.(Model)
	if cmd != nil {
		m = update(m, cmd())
	}
	return ansi.Strip(strings.Join(m.renderPlayer(100), "\n"))
}

func TestLoggingInDaemon(t *testing.T) {
	text := playerText(t, loggingInDaemon(t, ""))
	if !strings.Contains(text, "logging in to Spotify") {
		t.Fatalf("a daemon without a session that is not ready must read as logging in:\n%s", text)
	}
}

func TestPairingCodeIsShown(t *testing.T) {
	text := playerText(t, loggingInDaemon(t, `{"url":"https://spotify.com/pair?code=ABC123","code":"ABC123","expires_at":"2026-09-29T20:00:00Z"}`))
	if !strings.Contains(text, "https://spotify.com/pair?code=ABC123") || !strings.Contains(text, "ABC123") {
		t.Fatalf("the pairing link and code must be shown:\n%s", text)
	}
}

func TestConnProblem(t *testing.T) {
	// A port nobody listens on refuses the connection.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	_, refused := api.New("http://" + addr).Status(context.Background())
	if h, _ := connProblem(refused); h != "go-librespot is not running" {
		t.Fatalf("refused: %q (%v)", h, refused)
	}

	// A daemon that takes the connection but never answers times out.
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, timedOut := api.New(hang.URL).Status(ctx)
	if h, hint := connProblem(timedOut); h != "go-librespot does not answer" || !strings.Contains(hint, "logging in") {
		t.Fatalf("timeout: %q / %q (%v)", h, hint, timedOut)
	}
}

func TestSessionClearsLoginState(t *testing.T) {
	m := newTestModel(t)
	m.loginKnown, m.loginAuth = true, &api.DeviceAuth{Code: "X"}
	m = update(m, status(testCtx, 1))
	if m.loginKnown || m.loginAuth != nil {
		t.Fatalf("a session must clear the login state")
	}
}

func TestLibraryReloadsOnceTheDaemonHasASession(t *testing.T) {
	m := New(api.New("http://127.0.0.1:1"), Options{Cover: CoverOff})
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(m, statusMsg{}) // reachable, no session yet
	m = update(m, playlistsMsg{err: api.ErrNoSession})
	m = update(m, albumsMsg{items: nil})

	nm, cmd := m.Update(status(testCtx, 1))
	m = nm.(Model)
	if cmd == nil || !m.library.playlistsState.loading {
		t.Fatalf("the failed playlists must load again once a session appears")
	}
	if m.library.albumsState.loading {
		t.Fatalf("listings that loaded must not load again")
	}

	nm, _ = m.Update(status(testCtx, 2))
	m = nm.(Model)
	m.library.playlistsState.loading = false
	if cmd := m.retryFailedLibrary(); cmd == nil {
		t.Fatalf("retryFailedLibrary still sees the failed listing")
	}
}
