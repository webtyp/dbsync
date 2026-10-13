//go:build !wasm

package ui

import (
	"webtyp.com/css"
	"webtyp.com/widget"
	"webtyp.com/widget/style"
)

// RenderCSS is the stylesheet of both widgets. It is a package function, not a
// method: the widgets are unexported, so the asset build can only reach their
// styles through the package.
func RenderCSS() *css.Stylesheet {
	return css.NewStylesheet(
		(&indicator{}).sheet().Stylesheet(),
		(&tray{}).sheet().Stylesheet(),
	)
}

func (i *indicator) sheet() *style.Sheet {
	return style.For(i).
		Root(style.Row(style.Space1)).
		Part(partToggle,
			style.Button(style.Bare),
			style.Row(style.Space1),
			style.Anchor(),
		).
		Part(partLabel,
			style.FontSize(style.TextSm),
			style.Anchor(),
		).
		When(widget.Open, partLabel, style.As(style.DangerWash))
}

func (t *tray) sheet() *style.Sheet {
	return style.For(t).
		Root(style.Stack(style.Space4)).
		Part(partRow,
			style.Row(style.Space3),
			style.ControlBox(),
		).
		Part(partReason, style.Grow()).
		Part(partEmpty,
			style.ControlBox(),
			style.FontSize(style.TextBase),
			style.CenterContent(),
		)
}
