//go:build windows

package main

import "syscall"

// systemLang returns "zh" when the Windows UI language is Chinese, "" otherwise.
// The original implementation only checked LANG-style environment variables,
// which are usually unset on Windows, so Chinese Windows users always saw
// English prompts.
func systemLang() lang {
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
