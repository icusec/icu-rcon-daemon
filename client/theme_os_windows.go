//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// detectDarkMode reports whether Windows is configured for dark apps
// (Settings > Personalization > Colors). Defaults to dark when unknown.
func detectDarkMode() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return v == 0
}
