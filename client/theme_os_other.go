//go:build !windows

package main

// detectDarkMode falls back to dark on non-Windows systems (GTK/macOS theme
// probing would require spawning helpers; dark matches the terminal look).
func detectDarkMode() bool {
	return true
}
