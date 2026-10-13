package ui

import (
	"strconv"

	"webtyp.com/components/countbadge"
	"webtyp.com/components/modaldialog"
	"webtyp.com/dbsync"
	. "webtyp.com/dom"
	. "webtyp.com/html"
	"webtyp.com/lang"
	"webtyp.com/widget"
)

const (
	NameIndicator = widget.Name("dbsyncindicator")
	PartLabel     = widget.Part("label")
)

var (
	clsIndicator = NameIndicator.Root()
	clsLabel     = NameIndicator.Class(PartLabel)
)

type indicator struct {
	Element
	client      *dbsync.Client
	online      *SignalBool
	offline     *SignalBool // mapped to !online to bind Open state
	pending     *SignalString
	rejected    *SignalString
	hasPending  *SignalBool
	hasRejected *SignalBool
	statusText  *SignalString
}

func Indicator(c *dbsync.Client) Component {
	i := &indicator{
		client:      c,
		online:      NewBool(false),
		offline:     NewBool(true),
		pending:     NewString("0"),
		rejected:    NewString("0"),
		hasPending:  NewBool(false),
		hasRejected: NewBool(false),
		statusText:  NewString(""),
	}

	st := c.Status()
	i.updateSignals(st)

	c.OnStatus(func(s dbsync.Status) {
		i.updateSignals(s)
	})

	return i
}

func (i *indicator) updateSignals(st dbsync.Status) {
	i.online.Set(st.Online)
	i.offline.Set(!st.Online)
	i.pending.Set(strconv.Itoa(st.Pending))
	i.rejected.Set(strconv.Itoa(st.Rejected))
	i.hasPending.Set(st.Pending > 0)
	i.hasRejected.Set(st.Rejected > 0)
	if st.Online {
		i.statusText.Set(lang.Translate(lang.Text("Online")).String())
	} else {
		i.statusText.Set(lang.Translate(lang.Text("Offline")).String())
	}
}

func (i *indicator) WidgetName() widget.Name { return NameIndicator }
func (i *indicator) WidgetKind() widget.Kind { return widget.Disclosure }

func (i *indicator) Render() *Element {
	modal := &modaldialog.ModalDialog{
		Title:   lang.Text("Conflicts"),
		Content: Tray(i.client),
	}

	badgePending := &countbadge.CountBadge{
		Count:   i.pending,
		Visible: i.hasPending,
	}

	badgeRejected := &countbadge.CountBadge{
		Count:   i.rejected,
		Visible: i.hasRejected,
	}

	btn := Button().
		Set(clsIndicator.AsAttr()).
		BindStateFunc(widget.Open, i.offline.Get).
		OnClick(func(e Event) {
			modal.Open()
		}).
		Child(
			Span().
				Set(clsLabel.AsAttr()).
				BindText(i.statusText),
		).
		Child(badgePending.Render()).
		Child(badgeRejected.Render()).
		Child(modal.Render())

	return btn
}
