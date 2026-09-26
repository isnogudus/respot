package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestKeyLineAlwaysAtBottom(t *testing.T) {
	cases := map[string]func(m *Model){
		"plain":   func(m *Model) {},
		"note":    func(m *Model) { m.note = "Queued: something" },
		"error":   func(m *Model) { m.setErr(errors.New("boom")) },
		"prompt":  func(m *Model) { m.mode = inputPlay },
		"help":    func(m *Model) { m.showHelp = true },
		"library": func(m *Model) { m.showHelp = true; m.pane = paneLibrary },
	}
	for name, setup := range cases {
		for _, height := range []int{12, 30} {
			m := newLibraryModel(t)
			m.pane, m.height = paneNone, height
			setup(&m)

			lines := strings.Split(m.View(), "\n")
			if len(lines) != height {
				t.Errorf("%s/%d: view has %d lines, want %d", name, height, len(lines), height)
			}
			last := ansi.Strip(lines[len(lines)-1])
			if !strings.Contains(last, "play/pause") || !strings.Contains(last, "quit") {
				t.Errorf("%s/%d: last line is %q, want the key line", name, height, last)
			}
		}
	}
}
