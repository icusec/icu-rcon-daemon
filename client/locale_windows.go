//go:build windows

package main

import "syscall"

// systemLang returns "zh" when the Windows UI language is Chinese, "" otherwise.
// GetUserDefaultUILanguage is resolved lazily so the binary keeps working on
// stripped-down systems where kernel32 lookups fail.
func systemLang() langKey {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetUserDefaultUILanguage")
	if err := proc.Find(); err != nil {
		return ""
	}
	id, _, _ := proc.Call()
	// LANGID: primary language is the low byte; 0x04 is Chinese.
	if id != 0 && id&0xff == 0x04 {
		return langZH
	}
	return ""
}
