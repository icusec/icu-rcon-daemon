package main

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// lineColor maps a log line kind to its text color and glyph prefix.
func lineColor(k logKind) (c color.NRGBA, prefix string) {
	switch k {
	case logCmd:
		return colCyan, "> "
	case logErr:
		return colRed, "× "
	case logSys:
		return colYellow, "· "
	default:
		return colText, ""
	}
}

func (u *ui) layoutOutput(gtx layout.Context, s *ServerEntry) layout.Dimensions {
	L := u.L
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Panel header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := label(u.th, unit.Sp(12), L.Output, colTextDim)
						t.Font.Weight = font.Bold
						return t.Layout(gtx)
					}),
					// Flexible spacer: must return its allotted width, or the
					// widgets after it pack against the previous child.
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnCopyAll, Text: L.CopyAll, Kind: btnSubtle, Small: true})
					}),
					layout.Rigid(space(gtx.Dp(6))),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnClearOut, Text: L.ClearOutput, Kind: btnSubtle, Small: true})
					}),
				)
			})
		}),
		// Log lines
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			logs := u.net.logsFor(s.ID)
			if len(logs) == 0 {
				lb := label(u.th, unit.Sp(12), outputHint(L), colTextFaint)
				lb.Alignment = text.Middle
				return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24)}.Layout(gtx, lb.Layout)
			}
			if len(u.logClicks) != len(logs) {
				u.logClicks = make([]*widget.Clickable, len(logs))
				for i := range u.logClicks {
					u.logClicks[i] = &widget.Clickable{}
				}
			}
			return material.List(u.th, &u.outList).Layout(gtx, len(logs), func(gtx layout.Context, i int) layout.Dimensions {
				return u.layoutLogLine(gtx, logs[i], u.logClicks[i])
			})
		}),
		// breathing room above the input bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(0, gtx.Dp(4))}
		}),
	)
}

func outputHint(L uiStrings) string {
	return "点击「测试连接」验证可达性；输入命令后回车执行。\n" +
		"点击任意输出行可复制其内容。"
}

func (u *ui) layoutLogLine(gtx layout.Context, line logLine, c *widget.Clickable) layout.Dimensions {
	col, prefix := lineColor(line.Kind)

	content := func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		g2 := gtx
		g2.Constraints.Min = image.Point{}
		inner := func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(16), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := label(u.th, unit.Sp(11), line.At.Format("15:04:05"), colTextFaint)
						return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, t.Layout)
					}),
					layout.Rigid(space(gtx.Dp(10))),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := prefix + line.Text
						if line.MS > 0 {
							txt += "   (" + strconv.Itoa(int(line.MS)) + "ms)"
						}
						t := label(u.th, unit.Sp(13), txt, col)
						return t.Layout(gtx)
					}),
				)
			})
		}
		dims := inner(g2)
		macros := m.Stop()

		if c.Hovered() {
			fillRect(gtx, image.Rectangle{Max: dims.Size}, 0, colCardHover)
		}
		macros.Add(gtx.Ops)
		return layout.Dimensions{Size: dims.Size}
	}
	return rowClickable(gtx, c, content)
}

func (u *ui) layoutInputBar(gtx layout.Context, s *ServerEntry) layout.Dimensions {
	L := u.L
	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(10), Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return chevron(gtx, gtx.Dp(11), gtx.Dp(2), colAccent)
				})
			}),
			// Input field with drawn background.
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				m := op.Record(gtx.Ops)
				g2 := gtx
				g2.Constraints.Min = image.Point{}
				padX := gtx.Dp(12)
				// Fill the flex share exactly, accounting for the padding that
				// is added back below (otherwise the row overflows).
				g2.Constraints.Min.X = gtx.Constraints.Max.X - padX*2
				ed := material.Editor(u.th, &u.input, L.InputHint)
				ed.Color = colText
				ed.HintColor = colTextFaint
				ed.TextSize = unit.Sp(13.5)
				dims := ed.Layout(g2)
				macros := m.Stop()
				padY := gtx.Dp(9)
				fillRect(gtx, image.Rectangle{Max: image.Pt(dims.Size.X+padX*2, dims.Size.Y+padY*2)}, gtx.Dp(8), colInputBg)
				defer op.Offset(image.Pt(padX, padY)).Push(gtx.Ops).Pop()
				macros.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(dims.Size.X+padX*2, dims.Size.Y+padY*2)}
			}),
			layout.Rigid(space(gtx.Dp(8))),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnUp, Text: "▲", Kind: btnGhost, Small: true})
			}),
			layout.Rigid(space(gtx.Dp(4))),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnDown, Text: "▼", Kind: btnGhost, Small: true})
			}),
			layout.Rigid(space(gtx.Dp(6))),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// While a command is in flight the button force-stops it.
				send := pillButton{C: &u.btnSend, Text: L.Send, Kind: btnPrimary}
				if s != nil && u.net.busy(s.ID) {
					send.Text = L.Stop
					send.Kind = btnDanger
				}
				return pillButtonLayout(gtx, u.th, send)
			}),
		)
	})
}

func (u *ui) layoutHistory(gtx layout.Context, s *ServerEntry) layout.Dimensions {
	L := u.L
	const histW = 232
	gtx.Constraints.Min.X = gtx.Dp(histW)
	gtx.Constraints.Max.X = gtx.Dp(histW)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(6), Left: unit.Dp(14), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := label(u.th, unit.Sp(12), L.History, colTextDim)
						t.Font.Weight = font.Bold
						return t.Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return pillButtonLayout(gtx, u.th, pillButton{C: &u.btnClearHist, Text: L.ClearHist, Kind: btnSubtle, Small: true})
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			hist := s.History
			if len(hist) == 0 {
				lb := label(u.th, unit.Sp(11.5), L.NoHistory, colTextFaint)
				lb.Alignment = text.Middle
				return layout.Inset{Left: unit.Dp(14), Right: unit.Dp(12)}.Layout(gtx, lb.Layout)
			}
			if len(u.histClicks) != len(hist) {
				u.syncHistClicks()
			}
			return material.List(u.th, &u.histList).Layout(gtx, len(hist), func(gtx layout.Context, i int) layout.Dimensions {
				c := u.histClicks[i]
				cmd := truncateRunes(hist[i], 28)
				content := func(gtx layout.Context) layout.Dimensions {
					m := op.Record(gtx.Ops)
					g2 := gtx
					g2.Constraints.Min = image.Point{}
					inner := func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(14), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							t := label(u.th, unit.Sp(12), cmd, colTextDim)
							t.MaxLines = 1
							return t.Layout(gtx)
						})
					}
					dims := inner(g2)
					macros := m.Stop()
					if c.Hovered() {
						fillRect(gtx, image.Rectangle{Max: dims.Size}, gtx.Dp(6), colCardHover)
					}
					macros.Add(gtx.Ops)
					return layout.Dimensions{Size: dims.Size}
				}
				return rowClickable(gtx, c, content)
			})
		}),
	)
}

func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
