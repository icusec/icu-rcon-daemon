package main

import (
	"image"
	"io"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type dialogKind int

const (
	dialogNone dialogKind = iota
	dialogEdit
	dialogDetails
	dialogConfirm
)

type ui struct {
	w   *app.Window
	th  *material.Theme
	st  *store
	net *rconService
	L   uiStrings

	servers []*ServerEntry
	sel     string // selected server id ("" = none)
	warn    string // persistent warning (e.g. corrupted store backup)

	// sidebar
	sidebarList widget.List
	btnAdd      widget.Clickable
	cardBtns    map[string]*widget.Clickable
	btnInfo     widget.Clickable
	btnEdit     widget.Clickable
	btnDel      widget.Clickable
	btnLang     widget.Clickable
	btnTheme    widget.Clickable

	// main panel
	btnTest      widget.Clickable
	btnSend      widget.Clickable
	btnUp        widget.Clickable
	btnDown      widget.Clickable
	btnCopyAll   widget.Clickable
	btnClearOut  widget.Clickable
	btnClearHist widget.Clickable
	input        widget.Editor
	outList      widget.List
	histList     widget.List
	logClicks    map[int]*widget.Clickable
	histClicks   []*widget.Clickable
	histIdx      int // -1 = live input; else index into history
	histDraft    string
	follow       bool // autoscroll output to bottom

	// dialogs
	dialog    dialogKind
	editID    string
	deleteID  string
	deleteNm  string
	formErr   string
	showPass  bool
	mode      widget.Enum
	dlgList   widget.List
	dName     widget.Editor
	dDHost    widget.Editor
	dDPort    widget.Editor
	dAuth     widget.Editor
	dRHost    widget.Editor
	dRPort    widget.Editor
	dRPass    widget.Editor
	dNote     widget.Editor
	btnSave   widget.Clickable
	btnCancel widget.Clickable
	btnClose  widget.Clickable
	btnEdit2  widget.Clickable
	btnShow   widget.Clickable
	btnYes    widget.Clickable
	btnNo     widget.Clickable
	scrim     widget.Clickable

	toast   string
	toastAt time.Time
}

func newUI(w *app.Window) *ui {
	u := &ui{
		w:         w,
		th:        newTheme(),
		L:         stringsFor(currentLang()),
		cardBtns:  map[string]*widget.Clickable{},
		logClicks: map[int]*widget.Clickable{},
		follow:    true,
		histIdx:   -1,
	}
	u.sidebarList.Axis = layout.Vertical
	u.outList.Axis = layout.Vertical
	u.outList.ScrollToEnd = true
	u.histList.Axis = layout.Vertical
	u.dlgList.Axis = layout.Vertical

	mk := func(e *widget.Editor) {
		e.SingleLine = true
	}
	mk(&u.input)
	u.input.Submit = true
	mk(&u.dName)
	mk(&u.dDHost)
	mk(&u.dDPort)
	mk(&u.dAuth)
	mk(&u.dRHost)
	mk(&u.dRPort)
	mk(&u.dRPass)
	mk(&u.dNote)
	return u
}

// reloadStore refreshes the cached server list and syncs per-card widgets.
// want selects the preferred selection id; empty means "keep current".
func (u *ui) reloadStore(want string) {
	u.servers = u.st.list()
	if want == "" {
		want = u.sel
	}
	if want == "" {
		want = u.st.lastSelected()
	}
	sel := ""
	for _, s := range u.servers {
		if s.ID == want {
			sel = s.ID
			break
		}
	}
	if sel == "" && len(u.servers) > 0 {
		sel = u.servers[0].ID
	}
	if sel != u.sel {
		// Row indexes are per server; stale hover/click state would otherwise
		// attach to unrelated rows after a selection change.
		u.logClicks = map[int]*widget.Clickable{}
	}
	u.sel = sel

	alive := map[string]bool{}
	for _, s := range u.servers {
		alive[s.ID] = true
		if _, ok := u.cardBtns[s.ID]; !ok {
			u.cardBtns[s.ID] = &widget.Clickable{}
		}
	}
	for id := range u.cardBtns {
		if !alive[id] {
			delete(u.cardBtns, id)
		}
	}
	u.syncHistClicks()
	if u.w != nil {
		u.w.Invalidate()
	}
}

func (u *ui) selected() *ServerEntry {
	for _, s := range u.servers {
		if s.ID == u.sel {
			return s
		}
	}
	return nil
}

func (u *ui) selectServer(id string) {
	if u.sel == id {
		return
	}
	u.sel = id
	u.histIdx = -1
	u.histDraft = ""
	u.follow = true
	u.logClicks = map[int]*widget.Clickable{}
	if id != "" {
		u.st.setLastSelected(id)
	}
	u.syncHistClicks()
}

func (u *ui) toastMsg(s string) {
	u.toast = s
	u.toastAt = time.Now()
	if u.w != nil {
		time.AfterFunc(2*time.Second, u.w.Invalidate)
	}
}

func (u *ui) copyText(gtx layout.Context, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(s))})
	u.toastMsg(u.L.Copied)
}

// clicked is a small helper over Clickable.Update.
func clicked(c *widget.Clickable, gtx layout.Context) bool {
	_, ok := c.Update(gtx)
	return ok
}

// handleEvents processes every widget interaction for this frame.
func (u *ui) handleEvents(gtx layout.Context) {
	if clicked(&u.btnAdd, gtx) {
		u.openEditDialog("")
	}

	for id, c := range u.cardBtns {
		cl, ok := c.Update(gtx)
		if !ok {
			continue
		}
		u.selectServer(id)
		if cl.NumClicks >= 2 {
			if s := u.selected(); s != nil {
				u.testConnect(s)
			}
		}
	}

	if clicked(&u.btnInfo, gtx) {
		u.openDetailsDialog()
	}
	if clicked(&u.btnEdit, gtx) {
		u.openEditDialog(u.sel)
	}
	if clicked(&u.btnDel, gtx) {
		if s := u.selected(); s != nil {
			u.deleteID, u.deleteNm = s.ID, s.Name
			u.dialog = dialogConfirm
			u.formErr = ""
		}
	}
	if clicked(&u.btnLang, gtx) {
		if currentLang() == langZH {
			langOverride = langEN
		} else {
			langOverride = langZH
		}
	}
	if clicked(&u.btnTheme, gtx) {
		applyTheme(!themeDark)
		pref := "dark"
		if !themeDark {
			pref = "light"
		}
		_ = u.st.setThemePref(pref)
	}

	if s := u.selected(); s != nil && clicked(&u.btnTest, gtx) {
		u.testConnect(s)
	}
	if clicked(&u.btnSend, gtx) {
		u.submitInput()
	}
	if clicked(&u.btnUp, gtx) {
		u.historyStep(+1)
	}
	if clicked(&u.btnDown, gtx) {
		u.historyStep(-1)
	}
	if clicked(&u.btnCopyAll, gtx) {
		if logs := u.net.logsFor(u.sel); len(logs) > 0 {
			var sb strings.Builder
			for _, l := range logs {
				sb.WriteString(l.At.Format("15:04:05 "))
				sb.WriteString(l.Text)
				sb.WriteByte('\n')
			}
			u.copyText(gtx, sb.String())
		}
	}
	if clicked(&u.btnClearOut, gtx) {
		u.net.clearLogs(u.sel)
		u.logClicks = map[int]*widget.Clickable{}
	}
	if clicked(&u.btnClearHist, gtx) {
		if u.sel != "" {
			_ = u.st.clearHistory(u.sel)
			u.histIdx = -1
			u.syncHistClicks()
		}
	}

	// Input editor events.
	for {
		ev, ok := u.input.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			u.submitInput()
		case widget.ChangeEvent:
			u.histIdx = -1
		}
	}

	// History arrow keys (when the editor leaves them unconsumed).
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &u.input, Name: key.NameUpArrow})
		if !ok {
			break
		}
		if _, isKey := ev.(key.Event); isKey {
			u.historyStep(+1)
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &u.input, Name: key.NameDownArrow})
		if !ok {
			break
		}
		if _, isKey := ev.(key.Event); isKey {
			u.historyStep(-1)
		}
	}

	// History list clicks.
	if hist := u.currentHistory(); hist != nil {
		for i, c := range u.histClicks {
			if i < len(hist) && clicked(c, gtx) {
				u.input.SetText(hist[i])
				u.histIdx = -1
			}
		}
	}

	// Log row clicks. Only rows visible in the last frame have a clickable;
	// iterating every row of a large scrollback would defeat the list's
	// virtualization and make each frame O(total rows).
	rows := u.net.rowsFor(u.sel)
	first := u.outList.Position.First
	last := first + u.outList.Position.Count
	if first < 0 {
		first = 0
	}
	if last > len(rows) {
		last = len(rows)
	}
	for i := first; i < last; i++ {
		if c, ok := u.logClicks[i]; ok && clicked(c, gtx) {
			u.copyText(gtx, rows[i].Text)
			break
		}
	}

	// Dialog interactions.
	if u.dialog != dialogNone {
		if clicked(&u.scrim, gtx) {
			u.closeDialog()
		}
		switch u.dialog {
		case dialogEdit:
			if clicked(&u.btnSave, gtx) {
				u.saveDialogForm()
			}
			if clicked(&u.btnCancel, gtx) {
				u.closeDialog()
			}
			if u.mode.Update(gtx) {
				u.formErr = ""
			}
		case dialogDetails:
			if clicked(&u.btnClose, gtx) {
				u.closeDialog()
			}
			if clicked(&u.btnEdit2, gtx) {
				u.openEditDialog(u.sel)
			}
			if clicked(&u.btnShow, gtx) {
				u.showPass = !u.showPass
			}
		case dialogConfirm:
			if clicked(&u.btnYes, gtx) {
				if err := u.st.delete(u.deleteID); err != nil {
					u.formErr = err.Error()
				} else {
					if u.deleteID == u.sel {
						u.selectServer("")
					}
					u.closeDialog()
					u.reloadStore("")
				}
			}
			if clicked(&u.btnNo, gtx) {
				u.closeDialog()
			}
		}
	}
}

func (u *ui) closeDialog() {
	u.dialog = dialogNone
	u.formErr = ""
	u.showPass = false
}

func (u *ui) testConnect(s *ServerEntry) {
	if u.net.busy(s.ID) {
		u.net.sysLog(s.ID, "… "+u.L.Connecting)
		return
	}
	u.follow = true
	u.net.send(s, "", true)
}

// currentHistory returns the selected server's history (may be nil).
func (u *ui) currentHistory() []string {
	if u.sel == "" {
		return nil
	}
	if e := u.st.get(u.sel); e != nil {
		return e.History
	}
	return nil
}

func (u *ui) syncHistClicks() {
	n := 0
	if h := u.currentHistory(); h != nil {
		n = len(h)
	}
	for len(u.histClicks) < n {
		u.histClicks = append(u.histClicks, &widget.Clickable{})
	}
	u.histClicks = u.histClicks[:n]
}

func (u *ui) historyStep(dir int) {
	h := u.currentHistory()
	if len(h) == 0 {
		return
	}
	if u.histIdx == -1 && dir > 0 {
		u.histDraft = u.input.Text()
		u.histIdx = 0
	} else {
		u.histIdx += dir
	}
	if u.histIdx >= len(h) {
		u.histIdx = len(h) - 1
	}
	if u.histIdx < 0 {
		u.histIdx = -1
		u.input.SetText(u.histDraft)
		return
	}
	u.input.SetText(h[u.histIdx])
}

func (u *ui) submitInput() {
	s := u.selected()
	if s == nil {
		return
	}
	// Busy: the send button doubles as a force-stop control.
	if u.net.busy(s.ID) {
		u.net.cancel(s.ID)
		return
	}
	cmd := strings.TrimSpace(u.input.Text())
	if cmd == "" {
		return
	}
	_ = u.st.pushHistory(s.ID, cmd)
	u.histIdx = -1
	u.histDraft = ""
	u.syncHistClicks()
	u.follow = true
	u.net.send(s, cmd, false)
	u.input.SetText("")
}

// ----------------------------- root layout --------------------------------

func (u *ui) layout(gtx layout.Context) {
	u.L = stringsFor(currentLang())
	// Process widget events BEFORE drawing: Clickable.Layout drains click
	// events for its own history, so an Update() after Layout sees nothing.
	u.handleEvents(gtx)

	fillRect(gtx, rectFrom(gtx), 0, colBg)

	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.layoutSidebar(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return u.layoutMain(gtx)
		}),
	)

	if u.warn != "" {
		bottom := gtx.Constraints.Max
		h := gtx.Dp(24)
		fillRect(gtx, image.Rectangle{Min: image.Pt(0, bottom.Y-h), Max: bottom}, 0, colYellow)
		defer op.Offset(image.Pt(gtx.Dp(10), bottom.Y-h+gtx.Dp(4))).Push(gtx.Ops).Pop()
		label(u.th, unit.Sp(11), u.warn, colBg).Layout(gtx)
	}

	if u.dialog != dialogNone {
		u.layoutDialog(gtx)
	}
}

func (u *ui) layoutSidebar(gtx layout.Context) layout.Dimensions {
	L := u.L
	sideW := gtx.Dp(288)
	gtx.Constraints.Min.X = sideW
	gtx.Constraints.Max.X = sideW

	fillRect(gtx, rectFrom(gtx), 0, colPanel)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(18), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := label(u.th, unit.Sp(17), L.AppName, colText)
						t.Font.Weight = font.Bold
						return t.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t2 := label(u.th, unit.Sp(11), L.AppSub, colTextFaint)
						t2.MaxLines = 2
						return t2.Layout(gtx)
					}),
				)
			})
		}),
		// Add button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnAdd, Text: L.AddServer, Kind: btnPrimary})
			})
		}),
		// Server list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if len(u.servers) == 0 {
					lb := label(u.th, unit.Sp(12), L.EmptyList, colTextFaint)
					lb.Alignment = text.Middle
					return lb.Layout(gtx)
				}
				return material.List(u.th, &u.sidebarList).Layout(gtx, len(u.servers), func(gtx layout.Context, i int) layout.Dimensions {
					return u.layoutServerCard(gtx, u.servers[i])
				})
			})
		}),
		// Selection actions
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.sel == "" {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnInfo, Text: L.Details, Kind: btnGhost, Small: true})
					}),
					layout.Rigid(space(gtx.Dp(6))),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnEdit, Text: L.Edit, Kind: btnGhost, Small: true})
					}),
					layout.Rigid(space(gtx.Dp(6))),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnDel, Text: L.Delete, Kind: btnGhost, Small: true})
					}),
				)
			})
		}),
		// Footer
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return hline(gtx, colBorder)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return label(u.th, unit.Sp(11), L.Version+" "+clientVersion, colTextFaint).Layout(gtx)
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := L.LightTheme
									if themeDark {
										lbl = L.DarkTheme
									}
									return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnTheme, Text: lbl, Kind: btnSubtle, Small: true})
								}),
								layout.Rigid(space(gtx.Dp(6))),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnLang, Text: L.LangToggle, Kind: btnSubtle, Small: true})
								}),
							)
						})
					}),
				)
			})
		}),
	)
}

func space(v int) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(v, 0)}
	}
}

func (u *ui) layoutServerCard(gtx layout.Context, s *ServerEntry) layout.Dimensions {
	c := u.cardBtns[s.ID]
	sel := s.ID == u.sel
	pad := gtx.Dp(10)

	content := func(gtx layout.Context) layout.Dimensions {
		// Measure the inner content first, then paint the card background.
		m := op.Record(gtx.Ops)
		g2 := gtx
		g2.Constraints.Min = image.Point{}
		inner := func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(13), Right: unit.Dp(13)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								t := label(u.th, unit.Sp(13.5), s.Name, colText)
								t.MaxLines = 1
								t.Font.Weight = font.Bold
								return t.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								col := colTextFaint
								if u.net.busy(s.ID) {
									col = colYellow
								}
								return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return statusDot(gtx, col, gtx.Dp(7))
								})
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									name := u.L.ModeDaemon
									if s.Mode == modeDirect {
										name = u.L.ModeDirect
									}
									return badge(gtx, u.th, name, colAccentDim, colCyan)
								}),
								layout.Rigid(space(gtx.Dp(8))),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									t := label(u.th, unit.Sp(11), s.Address(), colTextDim)
									t.MaxLines = 1
									return t.Layout(gtx)
								}),
							)
						})
					}),
				)
			})
		}
		dims := inner(g2)
		macros := m.Stop()

		bg := colCard
		if sel {
			bg = colCardSel
		}
		if c.Hovered() {
			bg = colCardHover
		}
		w := dims.Size.X + pad*2
		h := dims.Size.Y
		fillRect(gtx, image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(10), bg)
		if sel {
			fillRect(gtx, image.Rectangle{Max: image.Pt(gtx.Dp(3), h)}, gtx.Dp(1), colAccent)
		}
		defer op.Offset(image.Pt(pad, 0)).Push(gtx.Ops).Pop()
		macros.Add(gtx.Ops)
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	return rowClickable(gtx, c, content)
}

func (u *ui) layoutMain(gtx layout.Context) layout.Dimensions {
	s := u.selected()
	if s == nil {
		return u.layoutEmptyMain(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.layoutTopBar(gtx, s)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return hline(gtx, colBorder)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return u.layoutOutput(gtx, s)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return vline(gtx, colBorder)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return u.layoutHistory(gtx, s)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return hline(gtx, colBorder)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.layoutInputBar(gtx, s)
		}),
	)
}

func (u *ui) layoutEmptyMain(gtx layout.Context) layout.Dimensions {
	L := u.L
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// Drawn chevron (the bundled CJK font has no U+276F glyph).
				sz := gtx.Dp(52)
				return layout.Inset{Left: unit.Dp(14), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return chevron(gtx, sz, gtx.Dp(7), colAccentDim)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := label(u.th, unit.Sp(14), L.NotSelected, colTextDim)
				t.Alignment = text.Middle
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, t.Layout)
			}),
		)
	})
}

func (u *ui) layoutTopBar(gtx layout.Context, s *ServerEntry) layout.Dimensions {
	L := u.L
	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := label(u.th, unit.Sp(15), s.Name, colText)
						t.Font.Weight = font.Bold
						t.MaxLines = 1
						return t.Layout(gtx)
					}),
					layout.Rigid(space(gtx.Dp(10))),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						name := L.ModeDaemon
						if s.Mode == modeDirect {
							name = L.ModeDirect
						}
						return layout.Inset{Top: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return badge(gtx, u.th, name, colAccentDim, colCyan)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = image.Point{}
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnTest, Text: L.TestConn, Kind: btnPrimary, Small: true})
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								col := colTextFaint
								res := u.lastResult()
								switch {
								case u.net.busy(s.ID):
									col = colYellow
								case res == "err":
									col = colRed
								case res == "cancel":
									col = colYellow
								case res == "resp":
									col = colGreen
								}
								return statusDot(gtx, col, gtx.Dp(7))
							})
						}),
						layout.Rigid(space(gtx.Dp(7))),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							var txt string
							ms := u.lastLatency()
							switch res := u.lastResult(); {
							case u.net.busy(s.ID):
								txt = L.Connecting
							case res == "err":
								txt = L.ConnFail
								if ms > 0 {
									txt += " · " + strconv.Itoa(int(ms)) + "ms"
								}
							case res == "cancel":
								txt = L.Stopped
							case res == "resp":
								txt = L.ConnOK
								if ms > 0 {
									txt += " · " + strconv.Itoa(int(ms)) + "ms"
								}
							default:
								txt = L.Idle
							}
							return label(u.th, unit.Sp(11.5), txt, colTextDim).Layout(gtx)
						}),
						layout.Rigid(space(gtx.Dp(12))),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return label(u.th, unit.Sp(11), s.Address(), colTextFaint).Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

// lastResult classifies the most recent outcome: "", "resp", "err" or
// "cancel" (a force-stopped command).
func (u *ui) lastResult() string {
	logs := u.net.logsFor(u.sel)
	for i := len(logs) - 1; i >= 0 && i >= len(logs)-8; i-- {
		switch logs[i].Kind {
		case logResp:
			return "resp"
		case logErr:
			return "err"
		case logSys:
			t := logs[i].Text
			if strings.HasPrefix(t, "√ ") {
				return "resp"
			}
			if strings.Contains(t, u.L.Cancelled) {
				return "cancel"
			}
		}
	}
	return ""
}

func (u *ui) lastLatency() int64 {
	logs := u.net.logsFor(u.sel)
	for i := len(logs) - 1; i >= 0 && i >= len(logs)-6; i-- {
		if logs[i].MS > 0 {
			return logs[i].MS
		}
	}
	return 0
}
