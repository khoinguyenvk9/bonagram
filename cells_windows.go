//go:build windows

package main

// termCellPixels: the Windows console does not report pixel sizes.
func termCellPixels() (int, int) { return 0, 0 }
