//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// termCellPixels is the cell size the terminal reports with the window size (0, 0 when it does not).
func termCellPixels() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}
