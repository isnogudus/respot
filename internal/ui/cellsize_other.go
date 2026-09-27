//go:build !unix

package ui

// terminalCellAspect is unknown on this platform.
func terminalCellAspect() float64 { return 0 }
