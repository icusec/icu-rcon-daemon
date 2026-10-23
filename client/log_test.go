package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSplitDisplayRows(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{""}},
		{"single line", "hello", []string{"hello"}},
		{"newlines", "a\nb\nc", []string{"a", "b", "c"}},
		{"trailing newline", "a\nb\n", []string{"a", "b"}},
		{"blank line kept", "a\n\nb", []string{"a", "", "b"}},
		{"only newlines", "\n\n", []string{"", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitDisplayRows(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("row %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}

	t.Run("long ascii line is chunked", func(t *testing.T) {
		in := strings.Repeat("x", maxRowRunes*2+5)
		rows := splitDisplayRows(in)
		if len(rows) != 3 || len(rows[0]) != maxRowRunes || len(rows[2]) != 5 {
			t.Fatalf("chunks = %d/%d/%d", len(rows[0]), len(rows[1]), len(rows[2]))
		}
		if strings.Join(rows, "") != in {
			t.Fatal("chunking lost data")
		}
	})

	t.Run("long utf8 line keeps runes intact", func(t *testing.T) {
		in := strings.Repeat("汉", maxRowRunes+3)
		rows := splitDisplayRows(in)
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2", len(rows))
		}
		for _, row := range rows {
			if !utf8.ValidString(row) {
				t.Fatalf("invalid utf8 row: %q", row)
			}
		}
		if strings.Join(rows, "") != in {
			t.Fatal("chunking lost data")
		}
	})
}

func TestLogRowsAndFirstFlags(t *testing.T) {
	s := newRconService(nil)
	now := time.Now()
	s.append("srv", logLine{Kind: logCmd, Text: "help", At: now})
	s.append("srv", logLine{Kind: logResp, Text: "a\nb", At: now, MS: 7})

	rows := s.rowsFor("srv")
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if !rows[0].First || rows[0].Text != "help" || rows[0].Kind != logCmd {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if !rows[1].First || rows[1].Text != "a" || rows[1].MS != 7 {
		t.Fatalf("row 1 = %+v", rows[1])
	}
	if rows[2].First || rows[2].Text != "b" {
		t.Fatalf("row 2 = %+v", rows[2])
	}
	if got := len(s.logsFor("srv")); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
}

// TestLargeResponseRowsBounded proves a single 120 KB listgamemodeproperties
// reply is not kept as one giant row: the UI then lays out only the visible
// rows instead of shaping the whole response on every frame.
func TestLargeResponseRowsBounded(t *testing.T) {
	s := newRconService(nil)
	text := strings.Repeat("x", 120_000)
	s.append("srv", logLine{Kind: logResp, Text: text, At: time.Now()})

	rows := s.rowsFor("srv")
	want := (len(text) + maxRowRunes - 1) / maxRowRunes
	if len(rows) != want {
		t.Fatalf("rows = %d, want %d", len(rows), want)
	}
	if len(rows[0].Text) > maxRowRunes {
		t.Fatalf("first row has %d runes", len(rows[0].Text))
	}
}

func TestLogByteBudgetDropsOldest(t *testing.T) {
	old := maxLogBytes
	maxLogBytes = 100
	defer func() { maxLogBytes = old }()

	s := newRconService(nil)
	s.append("srv", logLine{Text: strings.Repeat("a", 60)})
	s.append("srv", logLine{Text: strings.Repeat("b", 60)})
	s.append("srv", logLine{Text: strings.Repeat("c", 60)})

	entries := s.logsFor("srv")
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Text, "c") {
		t.Fatalf("entries = %d, want the newest one", len(entries))
	}
	rows := s.rowsFor("srv")
	if len(rows) != 1 || !strings.HasPrefix(rows[0].Text, "c") {
		t.Fatalf("rows did not follow the trimmed entries: %d rows", len(rows))
	}
}

func TestLogEntryLimitDropsOldest(t *testing.T) {
	s := newRconService(nil)
	for i := 0; i < logLimit+5; i++ {
		s.append("srv", logLine{Kind: logSys, Text: "line", At: time.Now()})
	}
	if got := len(s.logsFor("srv")); got != logLimit {
		t.Fatalf("entries = %d, want %d", got, logLimit)
	}
	if got := len(s.rowsFor("srv")); got != logLimit {
		t.Fatalf("rows = %d, want %d", got, logLimit)
	}
}

func TestClearLogsClearsRows(t *testing.T) {
	s := newRconService(nil)
	s.append("srv", logLine{Text: "a\nb"})
	s.clearLogs("srv")
	if len(s.logsFor("srv")) != 0 || len(s.rowsFor("srv")) != 0 {
		t.Fatal("clearLogs left entries or rows behind")
	}
}

func TestTestDescriptionsFollowLanguage(t *testing.T) {
	old := langOverride
	defer func() { langOverride = old }()

	e := &ServerEntry{Mode: modeDaemon, DaemonHost: "127.0.0.1", DaemonPort: 8080}
	langOverride = langEN
	if got := testDesc(e); !strings.Contains(got, "Test connection") {
		t.Fatalf("english testDesc = %q", got)
	}
	if got := testOKDesc(7); !strings.Contains(got, "succeeded") {
		t.Fatalf("english testOKDesc = %q", got)
	}
	langOverride = langZH
	if got := testDesc(e); !strings.Contains(got, "测试连接") {
		t.Fatalf("chinese testDesc = %q", got)
	}
	if got := testOKDesc(7); !strings.Contains(got, "测试成功") {
		t.Fatalf("chinese testOKDesc = %q", got)
	}
}
