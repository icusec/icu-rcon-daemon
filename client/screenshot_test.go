package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/op"
	"gioui.org/unit"
)

// The docs screenshots are rendered with Gio's headless backend, so they need
// no window manager and never contain a title bar or black capture borders.
//
// Regenerate them with:
//
//	cd client
//	ICU_GEN_SCREENSHOTS=1 go test -run TestGenerateDocScreenshots -count=1
const (
	screenshotWidth  = 1344
	screenshotHeight = 835
)

func screenshotServerName(l langKey) string {
	if l == langEN {
		return "Sandstorm Test"
	}
	return "沙暴测试服"
}

func screenshotUI(t *testing.T, l langKey, withServer bool) (*ui, *ServerEntry) {
	t.Helper()
	langOverride = l
	applyTheme(false)

	st, _, err := loadStoreAt(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	u := newUI(nil)
	u.st = st
	u.net = newRconService(nil)
	if !withServer {
		return u, nil
	}

	e := &ServerEntry{
		Name:       screenshotServerName(l),
		Mode:       modeDaemon,
		DaemonHost: "127.0.0.1",
		DaemonPort: 8080,
		AuthKey:    "doc-demo-key-1234",
		RconHost:   "127.0.0.1",
		RconPort:   25575,
		RconPass:   "rcon-password",
		CreatedAt:  time.Date(2026, 10, 24, 1, 36, 0, 0, time.Local),
	}
	if err := st.add(e); err != nil {
		t.Fatalf("add server: %v", err)
	}
	u.reloadStore(e.ID)
	return u, e
}

// populateConnected builds the same short session the tutorial walks through.
func populateConnected(t *testing.T, u *ui, e *ServerEntry) {
	t.Helper()
	start := time.Date(2026, 10, 24, 1, 36, 20, 0, time.Local)
	later := start.Add(4 * time.Second)

	u.net.append(e.ID, logLine{Kind: logCmd, Text: "help", At: start})
	u.net.append(e.ID, logLine{Kind: logSys, Text: "→ " + testDesc(e), At: start})
	u.net.append(e.ID, logLine{Kind: logSys, Text: "√ " + testOKDesc(16), At: start, MS: 16})
	u.net.append(e.ID, logLine{Kind: logResp, Text: "OK:help", At: start, MS: 16})
	u.net.append(e.ID, logLine{Kind: logCmd, Text: "help", At: later})
	u.net.append(e.ID, logLine{Kind: logResp, Text: "OK:help", At: later, MS: 7})
	if err := u.st.pushHistory(e.ID, "help"); err != nil {
		t.Fatalf("history: %v", err)
	}
}

func renderScreenshot(t *testing.T, u *ui, path string) {
	t.Helper()
	size := image.Pt(screenshotWidth, screenshotHeight)

	var ops op.Ops
	// A few passes let the lists settle their scroll-to-end position and item
	// heights before the final frame is captured.
	for i := 0; i < 3; i++ {
		ops.Reset()
		gtx := app.NewContext(&ops, app.FrameEvent{
			Now:    time.Now(),
			Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Size:   size,
			Source: input.Source{},
			Frame:  func(*op.Ops) {},
		})
		u.layout(gtx)
	}

	hw, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer hw.Release()
	if err := hw.Frame(&ops); err != nil {
		t.Fatalf("render: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	if err := hw.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	t.Logf("wrote %s", path)
}

func TestGenerateDocScreenshots(t *testing.T) {
	if os.Getenv("ICU_GEN_SCREENSHOTS") != "1" {
		t.Skip("set ICU_GEN_SCREENSHOTS=1 to regenerate docs screenshots")
	}
	dir := os.Getenv("ICU_SCREENSHOT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "docs", "img")
	}

	oldLang, oldDark := langOverride, themeDark
	defer func() {
		langOverride = oldLang
		applyTheme(oldDark)
	}()

	langs := []struct {
		lang langKey
		sub  string
	}{
		{langZH, ""},
		{langEN, "en"},
	}

	for _, l := range langs {
		out := dir
		if l.sub != "" {
			out = filepath.Join(dir, l.sub)
		}

		empty, _ := screenshotUI(t, l.lang, false)
		renderScreenshot(t, empty, filepath.Join(out, "tutorial-empty.png"))

		connected, e := screenshotUI(t, l.lang, true)
		populateConnected(t, connected, e)
		renderScreenshot(t, connected, filepath.Join(out, "tutorial-connected.png"))

		add, _ := screenshotUI(t, l.lang, true)
		add.openEditDialog("")
		add.dName.SetText(screenshotServerName(l.lang))
		add.dDHost.SetText("127.0.0.1")
		add.dDPort.SetText("8080")
		add.dAuth.SetText("doc-demo-key-1234")
		add.dRHost.SetText("127.0.0.1")
		add.dRPort.SetText("25575")
		add.dRPass.SetText("rcon-password")
		renderScreenshot(t, add, filepath.Join(out, "tutorial-add.png"))
	}
}
