package ui

import (
	"webtyp.com/components/actionbutton"
	"webtyp.com/dbsync"
	. "webtyp.com/dom"
	. "webtyp.com/html"
	"webtyp.com/lang"
	"webtyp.com/time"
	"webtyp.com/widget"
)

const (
	NameTray   = widget.Name("dbsynctray")
	PartRow    = widget.Part("row")
	PartReason = widget.Part("reason")
	PartEmpty  = widget.Part("empty")
)

var (
	clsTray   = NameTray.Root()
	clsRow    = NameTray.Class(PartRow)
	clsReason = NameTray.Class(PartReason)
	clsEmpty  = NameTray.Class(PartEmpty)
)

type tray struct {
	Element
	client *dbsync.Client
	rows   *SignalNodes
}

func Tray(c *dbsync.Client) Component {
	t := &tray{
		client: c,
		rows:   NewNodes(),
	}

	t.refresh()

	c.OnStatus(func(_ dbsync.Status) {
		t.refresh()
	})

	return t
}

func (t *tray) WidgetName() widget.Name { return NameTray }
func (t *tray) WidgetKind() widget.Kind { return widget.Region }

func (t *tray) refresh() {
	muts, err := t.client.Rejected()
	if err != nil {
		t.rows.Set([]*Element{Div().Set(clsRow.AsAttr()).Text(err.Error())})
		return
	}

	if len(muts) == 0 {
		t.rows.Set([]*Element{
			Div().
				Set(clsEmpty.AsAttr()).
				Text(lang.Translate(lang.Text("No conflicts")).String()),
		})
		return
	}

	var elements []*Element
	for _, mut := range muts {
		// Create a local copy of m to avoid closure capture issues
		m := mut

		row := Div().Set(clsRow.AsAttr())

		// Date
		dateStr := time.FormatDateTime(m.CreatedAt)

		row.Child(Span().Text(dateStr))
		row.Child(Span().Text(" " + m.Op + " "))

		reasonSpan := Span().Set(clsReason.AsAttr()).Text(m.Reason)
		row.Child(reasonSpan)

		dismissBtn := &actionbutton.ActionButton{
			Text:    lang.Text("Dismiss"),
			Variant: "secondary",
			OnClick: func(_ Event) {
				if dismissErr := t.client.Dismiss(m.Id); dismissErr != nil {
					reasonSpan.Text(m.Reason + " (" + dismissErr.Error() + ")")
				} else {
					t.refresh()
				}
			},
		}
		row.Child(dismissBtn.Render())

		elements = append(elements, row)
	}

	t.rows.Set(elements)
}

func (t *tray) Render() *Element {
	// To serialize in SSR tests since BindChildren is WASM-only
	el := Div().Set(clsTray.AsAttr()).BindChildren(t.rows)
	for _, child := range t.rows.Get() {
		el.Child(child)
	}
	return el
}
