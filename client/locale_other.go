//go:build !windows

package main

// systemLang is unused on non-Windows platforms where LANG-style environment
// variables are checked directly.
func systemLang() langKey {
	return ""
}
