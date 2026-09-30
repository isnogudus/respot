package ui

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isnogudus/respot/internal/api"
)

// loginMsg reports whether a daemon without a session is ready and which
// pairing code, if any, it waits on.
type loginMsg struct {
	ready bool
	auth  *api.DeviceAuth
	err   error
}

// fetchLogin asks a daemon without a session how its login stands.
func (m Model) fetchLogin() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ready, err := c.Ready(ctx)
		if err != nil {
			return loginMsg{err: err}
		}
		auth, err := c.AuthCode(ctx)
		return loginMsg{ready: ready, auth: auth, err: err}
	}
}

// connProblem says in words why the daemon cannot be talked to.
func connProblem(err error) (headline, hint string) {
	var netErr net.Error
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "go-librespot is not running", "Start the daemon; retrying every few seconds."
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "go-librespot does not answer", "It may still be logging in to Spotify; retrying every few seconds."
	}
	return "Cannot reach go-librespot: " + err.Error(), "Retrying every few seconds."
}

// loginLines explains a daemon that answers but has no Spotify session.
func (m Model) loginLines() []string {
	if a := m.loginAuth; a != nil {
		lines := []string{
			mutedStyle.Render("Connect go-librespot to Spotify: open ") + accentStyle.Render(a.URL),
			mutedStyle.Render("and, if asked, enter the code ") + titleStyle.Render(a.Code),
		}
		if !a.ExpiresAt.IsZero() {
			lines[1] += subtleStyle.Render(" (valid until " + a.ExpiresAt.Local().Format(time.Kitchen) + ")")
		}
		return lines
	}
	if m.loginKnown && !m.loginReady {
		return []string{mutedStyle.Render("go-librespot is logging in to Spotify …"),
			subtleStyle.Render("It connects on its own once Spotify answers; see its log if this takes long.")}
	}
	return []string{mutedStyle.Render("No active Spotify session."),
		subtleStyle.Render("Select this device in a Spotify app to start.")}
}
