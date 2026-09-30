// Command my-spotify-tui is a terminal UI for the go-librespot daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/isnogudus/respot/internal/api"
	"github.com/isnogudus/respot/internal/ui"
)

func main() {
	defAddr := os.Getenv("LIBRESPOT_ADDR")
	if defAddr == "" {
		defAddr = "http://localhost:3678"
	}
	addr := flag.String("addr", defAddr, "go-librespot API base URL (env LIBRESPOT_ADDR)")
	cover := flag.String("cover", "auto", "album cover: auto, iterm2, blocks or off")
	flag.Parse()

	coverMode, err := ui.ParseCoverMode(*cover)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	client := api.New(*addr)
	p := tea.NewProgram(ui.New(client, ui.Options{Cover: coverMode}), tea.WithAltScreen())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.StreamEvents(ctx,
		func(ev api.Event) { p.Send(ui.EventMsg(ev)) },
		func(connected bool) { p.Send(ui.WSStateMsg(connected)) },
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
