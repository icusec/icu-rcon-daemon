// ICU RCON Client — a native Go GUI client for ICURconDaemon.
//
// It speaks the daemon's documented HTTP API (POST /api/icu/daemon/rcon) and
// can also talk the Source RCON protocol directly. Servers are stored in
// servers.json next to the executable for one-click reconnects.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"
)

// clientVersion is overridden at release build time via -ldflags.
var clientVersion = "v1.2.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--selftest" {
		runSelfTest()
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "--autotest" {
		runAutoTest(os.Args[2])
		return
	}

	go func() {
		w := new(app.Window)
		w.Option(
			app.Title(stringsFor(currentLang()).AppName),
			app.Size(unit.Dp(1180), unit.Dp(720)),
			app.MinSize(unit.Dp(920), unit.Dp(580)),
		)
		if err := run(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	// Load the saved servers before building any UI state.
	st, corrupt, err := loadStore()
	// Theme: explicit preference wins, otherwise follow the system.
	switch st.themePref() {
	case "dark":
		applyTheme(true)
	case "light":
		applyTheme(false)
	default:
		applyTheme(detectDarkMode())
	}
	ui := newUI(w)
	ui.st = st
	ui.net = newRconService(w)
	if err != nil {
		ui.warn = err.Error()
	} else if corrupt != "" {
		ui.warn = sprintf(stringsFor(currentLang()).ErrLoad, corrupt)
	}
	ui.reloadStore("")

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.layout(gtx)
			e.Frame(&ops)
		}
	}
}

// runAutoTest sends one command to the last-selected server synchronously and
// prints the result. It exercises the real network path (HTTP daemon or direct
// RCON) without opening a window:
//
//	ICURconClient.exe --autotest list
func runAutoTest(cmd string) {
	st, _, err := loadStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "load servers.json:", err)
		os.Exit(1)
	}
	id := st.lastSelected()
	if id == "" {
		if l := st.list(); len(l) > 0 {
			id = l[0].ID
		}
	}
	e := st.get(id)
	if e == nil {
		fmt.Fprintln(os.Stderr, "no server available (add one in the GUI first)")
		os.Exit(1)
	}
	fmt.Printf("target: %s (%s)\ncommand: %s\n", e.Name, e.Mode, cmd)
	start := time.Now()
	var text string
	ctx := context.Background()
	if e.Mode == modeDirect {
		text, err = execDirect(ctx, e, cmd)
	} else {
		text, err = execDaemon(ctx, e, cmd)
	}
	ms := time.Since(start).Milliseconds()
	if err != nil {
		fmt.Printf("ERR (%dms): %v\n", ms, err)
		os.Exit(1)
	}
	fmt.Printf("RESP (%dms): %s\n", ms, text)
}

// runSelfTest exercises the non-GUI logic (store persistence, protocol
// encoding) so CI and users can verify a build without opening a window.
func runSelfTest() {
	ok := true
	fail := func(format string, args ...any) {
		ok = false
		fmt.Printf("FAIL "+format+"\n", args...)
	}

	// Store round-trip.
	s := &store{path: tempStorePath(), data: storeFile{Version: 1}}
	e := &ServerEntry{
		Name: "测试服", Mode: modeDaemon,
		DaemonHost: "127.0.0.1", DaemonPort: 8080, AuthKey: "k",
		RconHost: "10.0.0.2", RconPort: 25575, RconPass: "p",
	}
	e.ID = newUUID()
	if err := s.add(e); err != nil {
		fail("store add: %v", err)
	}
	if got := s.get(e.ID); got == nil || got.Name != "测试服" {
		fail("store get round-trip mismatch")
	}
	if err := s.pushHistory(e.ID, "list"); err != nil {
		fail("pushHistory: %v", err)
	}
	if err := s.pushHistory(e.ID, "list"); err != nil {
		fail("pushHistory dup: %v", err)
	}
	if len(s.get(e.ID).History) != 1 {
		fail("history dedupe failed")
	}
	loaded, corrupt, err := loadStoreAt(s.path)
	if err != nil || corrupt != "" || len(loaded.list()) != 1 {
		fail("loadStoreAt: err=%v corrupt=%q n=%d", err, corrupt, len(loaded.list()))
	}
	os.Remove(s.path)

	// UUID shape.
	id := newUUID()
	if len(id) != 36 || id[14] != '4' {
		fail("newUUID shape: %q", id)
	}

	if ok {
		fmt.Println("selftest OK")
	} else {
		os.Exit(1)
	}
}
