//go:build !wasm

package ui

import (
	"webtyp.com/css"
	"webtyp.com/widget"
	"webtyp.com/widget/style"
)

func RenderCSS() *css.Stylesheet {
	s1 := style.For(&indicator{}).
		Root(

			style.Button(style.Bare), // using bare as it is an unpainted button normally
			style.Row(style.Space1),
			style.CenterContent(),
		).
		Part(PartLabel, style.FontSize(style.TextSm)).
		When(widget.Open, PartLabel, style.As(style.DangerWash)).
		Stylesheet()

	s2 := style.For(&tray{}).
		Root(style.Stack(style.Space4)).
		Part(PartRow,
			style.Row(style.Space3),
			style.ControlBox(),
		).
		Part(PartReason, style.Grow()).
		Part(PartEmpty,
			style.ControlBox(),
			style.FontSize(style.TextBase),
			style.CenterContent(),
		).
		Stylesheet()

	return css.NewStylesheet(css.Raw(s1.String() + "\n" + s2.String()))
}
