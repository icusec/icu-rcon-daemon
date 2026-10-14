package main

import (
	"image"
	"fmt"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (u *ui) openEditDialog(id string) {
	u.editID = id
	u.formErr = ""
	u.showPass = false
	e := u.st.get(id)
	if e == nil {
		e = &ServerEntry{Mode: modeDaemon}
	}
	u.dName.SetText(e.Name)
	u.mode.Value = e.Mode
	u.dDHost.SetText(e.DaemonHost)
	u.dDPort.SetText(portStr(e.DaemonPort))
	u.dAuth.SetText(e.AuthKey)
	u.dRHost.SetText(e.RconHost)
	u.dRPort.SetText(portStr(e.RconPort))
	u.dRPass.SetText(e.RconPass)
	u.dNote.SetText(e.Note)
	u.dialog = dialogEdit
}

func (u *ui) openDetailsDialog() {
	if u.selected() == nil {
		return
	}
	u.showPass = false
	u.dialog = dialogDetails
}

func portStr(p int) string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(p)
}

func (u *ui) saveDialogForm() {
	L := u.L
	name := strings.TrimSpace(u.dName.Text())
	dHost := strings.TrimSpace(u.dDHost.Text())
	dPort := strings.TrimSpace(u.dDPort.Text())
	auth := strings.TrimSpace(u.dAuth.Text())
	rHost := strings.TrimSpace(u.dRHost.Text())
	rPort := strings.TrimSpace(u.dRPort.Text())
	rPass := u.dRPass.Text()
	note := strings.TrimSpace(u.dNote.Text())
	mode := u.mode.Value
	if mode != modeDirect {
		mode = modeDaemon
	}

	switch {
	case name == "":
		u.formErr = L.ErrNameReq
		return
	case rHost == "":
		u.formErr = L.ErrHostReq
		return
	}
	rp, err := strconv.Atoi(rPort)
	if err != nil || rp < 1 || rp > 65535 {
		u.formErr = L.ErrPortInv
		return
	}
	dp := 0
	if mode == modeDaemon {
		if dHost == "" {
			u.formErr = L.ErrHostReq
			return
		}
		if auth == "" {
			u.formErr = L.ErrAuthReq
			return
		}
		dp, err = strconv.Atoi(dPort)
		if err != nil || dp < 1 || dp > 65535 {
			u.formErr = L.ErrPortInv
			return
		}
	}

	if u.editID == "" {
		e := &ServerEntry{
			Name: name, Mode: mode,
			DaemonHost: dHost, DaemonPort: dp, AuthKey: auth,
			RconHost: rHost, RconPort: rp, RconPass: rPass, Note: note,
		}
		if err := u.st.add(e); err != nil {
			u.formErr = sprintf(L.ErrSave, err)
			return
		}
		u.closeDialog()
		u.reloadStore(e.ID)
		u.toastMsg("√ " + L.Save)
		return
	}
	err = u.st.update(u.editID, func(e *ServerEntry) {
		e.Name, e.Mode = name, mode
		e.DaemonHost, e.DaemonPort, e.AuthKey = dHost, dp, auth
		e.RconHost, e.RconPort, e.RconPass = rHost, rp, rPass
		e.Note = note
	})
	if err != nil {
		u.formErr = sprintf(L.ErrSave, err)
		return
	}
	u.closeDialog()
	u.reloadStore(u.editID)
	u.toastMsg("√ " + L.Save)
}

// layoutDialog draws the modal scrim and the active dialog card on top.
func (u *ui) layoutDialog(gtx layout.Context) {
	// Scrim: full-screen dimming, click closes.
	u.scrim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		fillRect(gtx, rectFrom(gtx), 0, colOverlay)
		return layout.Dimensions{Size: rectFrom(gtx).Max}
	})

	// Card, centered, drawn after the scrim so it sits on top.
	layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: rectFrom(gtx).Max} }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			switch u.dialog {
			case dialogEdit:
				return u.layoutEditDialog(gtx)
			case dialogDetails:
				return u.layoutDetailsDialog(gtx)
			case dialogConfirm:
				return u.layoutConfirmDialog(gtx)
			}
			return layout.Dimensions{}
		}),
	)
}

// dialogCard measures content and paints the card chrome around it. The
// content is recorded first and replayed after the background so the opaque
// card does not cover it.
func dialogCard(gtx layout.Context, content layout.Widget, width int) layout.Dimensions {
	g2 := gtx
	g2.Constraints.Max.X = width
	g2.Constraints.Max.Y -= 2 * gtx.Dp(24)
	if g2.Constraints.Max.Y < gtx.Dp(120) {
		g2.Constraints.Max.Y = gtx.Dp(120)
	}
	g2.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	dims := content(g2)
	macros := m.Stop()
	fillRect(gtx, image.Rectangle{Max: dims.Size}, gtx.Dp(14), colPanel)
	fillRect(gtx, image.Rectangle{Max: image.Pt(dims.Size.X, gtx.Dp(1))}, 0, colBorder)
	macros.Add(gtx.Ops)
	return dims
}

func (u *ui) dialogTitle(gtx layout.Context, txt string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(18), Left: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		t := label(u.th, unit.Sp(16), txt, colText)
		t.Font.Weight = font.Bold
		return t.Layout(gtx)
	})
}

func (u *ui) dialogButtons(gtx layout.Context, buttons ...pillButton) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(14), Left: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		items := make([]layout.FlexChild, 0, len(buttons)*2)
		for i, b := range buttons {
			if i > 0 {
				ii := i
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(gtx.Dp(8), 0)}
				}))
				_ = ii
			}
			bb := b
			items = append(items, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return pillButtonLayout(gtx, u.th, bb)
			}))
		}
		return layout.Flex{}.Layout(gtx, items...)
	})
}

// formField renders a label + single-line input with a drawn field background.
func (u *ui) formField(gtx layout.Context, labelText string, e *widget.Editor, hint string) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return label(u.th, unit.Sp(11.5), labelText, colTextDim).Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				m := op.Record(gtx.Ops)
				g2 := gtx
				g2.Constraints.Min = image.Point{}
				padX, padY := gtx.Dp(10), gtx.Dp(8)
				// Fill the row exactly, accounting for the padding below.
				g2.Constraints.Min.X = gtx.Constraints.Max.X - padX*2
				ed := material.Editor(u.th, e, hint)
				ed.Color = colText
				ed.HintColor = colTextFaint
				ed.TextSize = unit.Sp(13)
				dims := ed.Layout(g2)
				macros := m.Stop()
				fillRect(gtx, image.Rectangle{Max: image.Pt(dims.Size.X+padX*2, dims.Size.Y+padY*2)}, gtx.Dp(8), colInputBg)
				defer op.Offset(image.Pt(padX, padY)).Push(gtx.Ops).Pop()
				macros.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(dims.Size.X+padX*2, dims.Size.Y+padY*2)}
			}),
		)
	})
}

func runeLen(s string) int {
	return len([]rune(s))
}

func (u *ui) layoutEditDialog(gtx layout.Context) layout.Dimensions {
	L := u.L
	width := minInt(gtx.Constraints.Max.X, gtx.Dp(520))

	content := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.dialogTitle(gtx, orElse(u.editID == "", L.AddTitle, L.EditTitle))
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return material.List(u.th, &u.dlgList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
					var items []layout.FlexChild
					items = append(items,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.formField(gtx, L.FieldName, &u.dName, "")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return label(u.th, unit.Sp(11.5), L.FieldMode, colTextDim).Layout(gtx)
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return material.RadioButton(u.th, &u.mode, modeDaemon, L.ModeDaemon).Layout(gtx)
											}),
											layout.Rigid(space(gtx.Dp(12))),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return material.RadioButton(u.th, &u.mode, modeDirect, L.ModeDirect).Layout(gtx)
											}),
										)
									}),
								)
							})
						}),
					)
					if u.mode.Value != modeDirect {
						items = append(items,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.formField(gtx, L.FieldDHost, &u.dDHost, "127.0.0.1")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.formField(gtx, L.FieldDPort, &u.dDPort, "8080")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return u.formField(gtx, L.FieldAuthKey, &u.dAuth, "")
							}),
						)
					}
					items = append(items,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.formField(gtx, L.FieldRHost, &u.dRHost, "127.0.0.1")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.formField(gtx, L.FieldRPort, &u.dRPort, "25575")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									return u.formField(gtx, L.FieldRPass, &u.dRPass, "")
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.formField(gtx, L.FieldNote, &u.dNote, "")
						}),
					)
					if u.formErr != "" {
						items = append(items,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return label(u.th, unit.Sp(12), "! "+u.formErr, colRed).Layout(gtx)
								})
							}),
						)
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.dialogButtons(gtx,
					pillButton{C: &u.btnCancel, Text: L.Cancel, Kind: btnGhost},
					pillButton{C: &u.btnSave, Text: L.Save, Kind: btnPrimary},
				)
			}),
		)
	}
	return dialogCard(gtx, content, width)
}

func (u *ui) layoutDetailsDialog(gtx layout.Context) layout.Dimensions {
	L := u.L
	e := u.selected()
	if e == nil {
		u.closeDialog()
		return layout.Dimensions{}
	}
	width := minInt(gtx.Constraints.Max.X, gtx.Dp(480))

	row := func(k, v string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(120)
						return label(u.th, unit.Sp(12), k, colTextDim).Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						t := label(u.th, unit.Sp(12.5), v, colText)
						t.MaxLines = 3
						return t.Layout(gtx)
					}),
				)
			})
		})
	}

	modeName := L.ModeDaemon
	if e.Mode == modeDirect {
		modeName = L.ModeDirect
	}
	pass := strings.Repeat("●", runeLen(e.RconPass))
	if e.RconPass == "" {
		pass = "—"
	}

	content := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.dialogTitle(gtx, L.DetailsTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						row(L.FieldName, e.Name),
						row(L.FieldMode, modeName),
						row(L.FieldDHost, orElse(e.DaemonHost == "", "—", e.DaemonHost)),
						row(L.FieldDPort, portStr(e.DaemonPort)),
						row(L.FieldAuthKey, maskOr(e.AuthKey, u.showPass)),
						row(L.FieldRHost, orElse(e.RconHost == "", "—", e.RconHost)),
						row(L.FieldRPort, portStr(e.RconPort)),
						row(L.FieldRPass, pass),
						row(L.FieldNote, orElse(e.Note == "", "—", e.Note)),
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(20), Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return pillButtonLayout(gtx, u.th, pillButton{
						C: &u.btnShow,
						Text: orElse(u.showPass, L.HidePass, L.ShowPass) + " " + L.FieldRPass,
						Kind: btnSubtle, Small: true,
					})
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.dialogButtons(gtx,
					pillButton{C: &u.btnEdit2, Text: L.Edit, Kind: btnGhost},
					pillButton{C: &u.btnClose, Text: L.Close, Kind: btnPrimary},
				)
			}),
		)
	}
	return dialogCard(gtx, content, width)
}

func (u *ui) layoutConfirmDialog(gtx layout.Context) layout.Dimensions {
	L := u.L
	width := minInt(gtx.Constraints.Max.X, gtx.Dp(400))
	content := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.dialogTitle(gtx, L.DelTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return label(u.th, unit.Sp(13), sprintf(L.DelBody, u.deleteNm), colTextDim).Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.dialogButtons(gtx,
					pillButton{C: &u.btnNo, Text: L.Cancel, Kind: btnGhost},
					pillButton{C: &u.btnYes, Text: L.Delete, Kind: btnDanger},
				)
			}),
		)
	}
	return dialogCard(gtx, content, width)
}

// ---------- small helpers ----------

func maskOr(s string, show bool) string {
	if s == "" {
		return "—"
	}
	if show {
		return s
	}
	return strings.Repeat("●", runeLen(s))
}

func orElse(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
