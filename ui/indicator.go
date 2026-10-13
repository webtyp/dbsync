package ui

import (
	"webtyp.com/components/countbadge"
	"webtyp.com/components/modaldialog"
	"webtyp.com/dbsync"
	. "webtyp.com/dom"
	"webtyp.com/fmt"
	. "webtyp.com/html"
	"webtyp.com/lang"
	"webtyp.com/widget"
)

const (
	nameIndicator = widget.Name("dbsyncindicator")
	// partToggle is the button that opens the tray. The dialog is its sibling,
	// never its child: a click inside the dialog must not reach the button.
	partToggle = widget.Part("toggle")
	partLabel  = widget.Part("label")
)

const (
	textOnline    = lang.Text("Online")
	textOffline   = lang.Text("Offline")
	textConflicts = lang.Text("Conflicts")
)

var (
	clsIndicator = nameIndicator.Root()
	clsToggle    = nameIndicator.Class(partToggle)
	clsLabel     = nameIndicator.Class(partLabel)
)

// indicator binds widget.Open to "offline": the stylesheet tints the label
// while the device cannot reach the server.
type indicator struct {
	Element
	offline     *SignalBool
	label       *SignalString
	pending     *SignalString
	rejected    *SignalString
	hasPending  *SignalBool
	hasRejected *SignalBool
	dialog      *modaldialog.ModalDialog
}

// Indicator shows this device's sync state: "Online" or "Offline", the writes waiting to be sent,
// and the writes the server refused. Clicking it opens the Tray in a modal dialog.
func Indicator(c *dbsync.Client) Component {
	i := &indicator{
		offline:     NewBool(false),
		label:       NewString(""),
		pending:     NewString(""),
		rejected:    NewString(""),
		hasPending:  NewBool(false),
		hasRejected: NewBool(false),
		dialog:      &modaldialog.ModalDialog{Title: textConflicts, Content: Tray(c)},
	}
	i.show(c.Status())
	c.OnStatus(i.show)
	return i
}

func (i *indicator) show(s dbsync.Status) {
	label := textOnline
	if !s.Online {
		label = textOffline
	}
	i.offline.Set(!s.Online)
	i.label.Set(lang.Translate(label).String())
	i.pending.Set(fmt.Convert(s.Pending).String())
	i.rejected.Set(fmt.Convert(s.Rejected).String())
	i.hasPending.Set(s.Pending > 0)
	i.hasRejected.Set(s.Rejected > 0)
}

func (i *indicator) WidgetName() widget.Name { return nameIndicator }
func (i *indicator) WidgetKind() widget.Kind { return widget.Disclosure }

func (i *indicator) Render() *Element {
	// Each count rides its own anchor: pending on the label, refused on the
	// button, so both bubbles show at once without covering each other. The
	// text has its own span: a bound text replaces every child of its element.
	label := Span().Set(clsLabel.AsAttr()).
		Child(Span().BindText(i.label)).
		Child(&countbadge.CountBadge{Count: i.pending, Visible: i.hasPending})
	toggle := Button().Set(clsToggle.AsAttr()).
		Attr("type", "button").
		OnClick(func(Event) { i.dialog.Open() }).
		Child(label).
		Child(&countbadge.CountBadge{Count: i.rejected, Visible: i.hasRejected})
	return Div().Set(clsIndicator.AsAttr()).
		BindState(widget.Open, i.offline).
		Child(toggle).
		Child(i.dialog)
}
