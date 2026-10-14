package main

import (
	"embed"
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

//go:embed assets/fonts/NotoSansSC-Regular.ttf assets/fonts/NotoSansSC-Bold.ttf
var fontFS embed.FS

// Palette colors, selected by applyTheme. All UI code reads these vars, so a
// theme switch takes effect on the next frame.
var (
	colBg, colPanel, colCard, colCardHover, colCardSel, colBorder color.NRGBA
	colAccent, colAccentDim, colText, colTextDim, colTextFaint    color.NRGBA
	colGreen, colRed, colYellow, colCyan, colInputBg              color.NRGBA
	colOverlay, colWhite                                          color.NRGBA

	// themeDark reports the currently active variant.
	themeDark = true
)

// applyTheme switches between the dark and light palettes.
func applyTheme(dark bool) {
	themeDark = dark
	if dark {
		colBg = color.NRGBA{R: 0x0e, G: 0x11, B: 0x16, A: 0xff}        // window base
		colPanel = color.NRGBA{R: 0x15, G: 0x19, B: 0x20, A: 0xff}     // sidebar/panels
		colCard = color.NRGBA{R: 0x1c, G: 0x22, B: 0x2c, A: 0xff}      // cards/buttons
		colCardHover = color.NRGBA{R: 0x24, G: 0x2c, B: 0x38, A: 0xff} // cards/buttons
		colCardSel = color.NRGBA{R: 0x1e, G: 0x28, B: 0x3c, A: 0xff}   // cards/buttons
		colBorder = color.NRGBA{R: 0x2a, G: 0x32, B: 0x40, A: 0xff}
		colAccent = color.NRGBA{R: 0x4f, G: 0x8c, B: 0xff, A: 0xff}
		colAccentDim = color.NRGBA{R: 0x4f, G: 0x8c, B: 0xff, A: 0x30}
		colText = color.NRGBA{R: 0xe6, G: 0xed, B: 0xf3, A: 0xff}
		colTextDim = color.NRGBA{R: 0x8b, G: 0x94, B: 0x9e, A: 0xff}
		colTextFaint = color.NRGBA{R: 0x5b, G: 0x63, B: 0x6c, A: 0xff}
		colGreen = color.NRGBA{R: 0x3f, G: 0xb9, B: 0x50, A: 0xff}
		colRed = color.NRGBA{R: 0xf8, G: 0x51, B: 0x49, A: 0xff}
		colYellow = color.NRGBA{R: 0xd2, G: 0x99, B: 0x22, A: 0xff}
		colCyan = color.NRGBA{R: 0x79, G: 0xc0, B: 0xff, A: 0xff}
		colInputBg = color.NRGBA{R: 0x11, G: 0x15, B: 0x1c, A: 0xff}
		colOverlay = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x8c}
	} else {
		colBg = color.NRGBA{R: 0xf5, G: 0xf7, B: 0xfa, A: 0xff}
		colPanel = color.NRGBA{R: 0xeb, G: 0xef, B: 0xf4, A: 0xff}
		colCard = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		colCardHover = color.NRGBA{R: 0xe2, G: 0xe9, B: 0xf1, A: 0xff}
		colCardSel = color.NRGBA{R: 0xe4, G: 0xee, B: 0xfd, A: 0xff}
		colBorder = color.NRGBA{R: 0xd3, G: 0xda, B: 0xe2, A: 0xff}
		colAccent = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xed, A: 0xff}
		colAccentDim = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xed, A: 0x2e}
		colText = color.NRGBA{R: 0x1b, G: 0x21, B: 0x29, A: 0xff}
		colTextDim = color.NRGBA{R: 0x5a, G: 0x64, B: 0x70, A: 0xff}
		colTextFaint = color.NRGBA{R: 0x8a, G: 0x94, B: 0xa0, A: 0xff}
		colGreen = color.NRGBA{R: 0x14, G: 0x7c, B: 0x3b, A: 0xff}
		colRed = color.NRGBA{R: 0xcf, G: 0x22, B: 0x2e, A: 0xff}
		colYellow = color.NRGBA{R: 0x9a, G: 0x67, B: 0x00, A: 0xff}
		colCyan = color.NRGBA{R: 0x0a, G: 0x62, B: 0xc2, A: 0xff}
		colInputBg = color.NRGBA{R: 0xf0, G: 0xf3, B: 0xf8, A: 0xff}
		colOverlay = color.NRGBA{R: 0x10, G: 0x14, B: 0x1a, A: 0x66}
	}
	colWhite = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

func newTheme() *material.Theme {
	th := material.NewTheme()
	regRaw, errReg := fontFS.ReadFile("assets/fonts/NotoSansSC-Regular.ttf")
	boldRaw, errBold := fontFS.ReadFile("assets/fonts/NotoSansSC-Bold.ttf")
	faces := []text.FontFace{}
	if errReg == nil {
		if f, e := opentype.Parse(regRaw); e == nil {
			// Register under both the family name and the empty default
			// family so material labels (which request no typeface) match.
			faces = append(faces,
				text.FontFace{Font: font.Font{Typeface: "Noto Sans SC"}, Face: f},
				text.FontFace{Font: font.Font{}, Face: f},
			)
		}
	}
	if errBold == nil {
		if f, e := opentype.Parse(boldRaw); e == nil {
			faces = append(faces,
				text.FontFace{Font: font.Font{Typeface: "Noto Sans SC", Weight: font.Bold}, Face: f},
				text.FontFace{Font: font.Font{Weight: font.Bold}, Face: f},
			)
		}
	}
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
	th.TextSize = unit.Sp(13)
	th.Palette = material.Palette{
		Bg:         colBg,
		Fg:         colText,
		ContrastBg: colAccent,
		ContrastFg: colWhite,
	}
	return th
}

// fillRect paints a (rounded) rectangle of the given color.
func fillRect(gtx layout.Context, r image.Rectangle, radius int, col color.NRGBA) {
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: r.Size()}.Op())
}

// rectFrom returns the full-bleed rectangle for the current constraints.
func rectFrom(gtx layout.Context) image.Rectangle {
	return image.Rectangle{Max: image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Max.Y)}
}

// label is a themed text label shortcut.
func label(th *material.Theme, size unit.Sp, txt string, col color.NRGBA) material.LabelStyle {
	lb := material.Label(th, size, txt)
	lb.Color = col
	return lb
}

// btnKind selects a visual style for pillButton.
type btnKind int

const (
	btnPrimary btnKind = iota
	btnDanger
	btnGhost
	btnSubtle
)

type pillButton struct {
	C     *widget.Clickable
	Text  string
	Kind  btnKind
	Small bool
}

// pillButtonLayout draws a rounded, self-styled button that hugs its label.
// Every variant gets a 1dp outline so controls read as buttons; the outline is
// painted as a filled ring with the fill inset on top of it.
func pillButtonLayout(gtx layout.Context, th *material.Theme, b pillButton) layout.Dimensions {
	var bg, fg, border color.NRGBA
	switch b.Kind {
	case btnPrimary:
		bg, fg, border = colAccent, colWhite, colAccent
	case btnDanger:
		bg, fg, border = colRed, colWhite, colRed
	case btnGhost:
		bg = colCard
		if b.C.Hovered() {
			bg = colCardHover
		}
		fg, border = colText, colBorder
	default: // btnSubtle — quiet, but unmistakably a button
		bg = color.NRGBA{} // transparent until hovered
		if b.C.Hovered() {
			bg = colCardHover
		}
		fg, border = colTextDim, colBorder
	}
	if (b.Kind == btnPrimary || b.Kind == btnDanger) && b.C.Hovered() {
		bg = lighten(bg, 1.15)
	}

	radius := gtx.Dp(8)
	padX, padY := gtx.Dp(14), gtx.Dp(7)
	size := unit.Sp(13)
	if b.Small {
		padX, padY = gtx.Dp(10), gtx.Dp(4)
		size = unit.Sp(12)
	}

	content := func(gtx layout.Context) layout.Dimensions {
		// Measure the label first so the box hugs it.
		m := op.Record(gtx.Ops)
		gl := gtx
		gl.Constraints.Min = image.Point{}
		lb := label(th, size, b.Text, fg)
		ldims := lb.Layout(gl)
		macros := m.Stop()

		w, h := ldims.Size.X+padX*2+2, ldims.Size.Y+padY*2+2
		r := image.Rectangle{Max: image.Pt(w, h)}
		fillRect(gtx, r, radius, border)
		if bg.A > 0 {
			inner := image.Rectangle{Min: image.Pt(1, 1), Max: image.Pt(w-1, h-1)}
			fillRect(gtx, inner, max(0, radius-1), bg)
		}

		defer op.Offset(image.Pt(padX+1, padY+1)).Push(gtx.Ops).Pop()
		macros.Add(gtx.Ops)
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	return b.C.Layout(gtx, content)
}

// rowClickable wraps content in a clickable area covering its bounds.
func rowClickable(gtx layout.Context, c *widget.Clickable, content layout.Widget) layout.Dimensions {
	return c.Layout(gtx, content)
}

// lighten scales a color's RGB channels by f (keeping alpha).
func lighten(c color.NRGBA, f float32) color.NRGBA {
	clamp := func(v float32) uint8 {
		if v > 255 {
			return 255
		}
		if v < 0 {
			return 0
		}
		return uint8(v)
	}
	return color.NRGBA{
		R: clamp(float32(c.R) * f),
		G: clamp(float32(c.G) * f),
		B: clamp(float32(c.B) * f),
		A: c.A,
	}
}

// statusDot draws a small filled circle.
func statusDot(gtx layout.Context, col color.NRGBA, size int) layout.Dimensions {
	sz := image.Pt(size, size)
	defer clip.UniformRRect(image.Rectangle{Max: sz}, size/2).Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: sz}.Op())
	return layout.Dimensions{Size: sz}
}

// badge renders a small rounded label (used for connection mode).
func badge(gtx layout.Context, th *material.Theme, txt string, bg, fg color.NRGBA) layout.Dimensions {
	content := func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		gl := gtx
		gl.Constraints.Min = image.Point{}
		ldims := label(th, unit.Sp(10.5), txt, fg).Layout(gl)
		macros := m.Stop()
		padX, padY := gtx.Dp(7), gtx.Dp(3)
		w, h := ldims.Size.X+padX*2, ldims.Size.Y+padY*2
		fillRect(gtx, image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(6), bg)
		defer op.Offset(image.Pt(padX, padY)).Push(gtx.Ops).Pop()
		macros.Add(gtx.Ops)
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	return content(gtx)
}

// hline draws a horizontal divider.
func hline(gtx layout.Context, col color.NRGBA) layout.Dimensions {
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: sz}.Op())
	return layout.Dimensions{Size: sz}
}

// vline draws a vertical divider.
func vline(gtx layout.Context, col color.NRGBA) layout.Dimensions {
	sz := image.Pt(gtx.Dp(1), gtx.Constraints.Max.Y)
	paint.FillShape(gtx.Ops, col, clip.Rect{Max: sz}.Op())
	return layout.Dimensions{Size: sz}
}

// chevron draws a ">" shape with thick strokes. Used instead of the U+276F
// glyph, which the bundled Noto Sans SC font does not contain. Path.Line is
// a RELATIVE move, so the second segment descends back to the baseline.
func chevron(gtx layout.Context, size, thickness int, col color.NRGBA) layout.Dimensions {
	var p clip.Path
	p.Begin(gtx.Ops)
	p.Move(f32.Pt(0, 0))
	p.Line(f32.Pt(float32(size*2)/3, float32(size)/2))
	p.Line(f32.Pt(-float32(size*2)/3, float32(size)/2))
	st := clip.Stroke{Path: p.End(), Width: float32(thickness)}.Op()
	paint.FillShape(gtx.Ops, col, st)
	return layout.Dimensions{Size: image.Pt(size*2/3+thickness, size)}
}
