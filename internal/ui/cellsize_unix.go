//go:build unix

package ui

import (
	"os"

	"golang.org/x/sys/unix"
)

// terminalCellAspect is a cell's height over its width in pixels, or 0 when
// the terminal does not report its pixel size.
func terminalCellAspect() float64 {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Row == 0 || ws.Col == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0
	}
	return (float64(ws.Ypixel) / float64(ws.Row)) / (float64(ws.Xpixel) / float64(ws.Col))
}
