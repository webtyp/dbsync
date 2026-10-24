package ui

import (
	"sync"

	"webtyp.com/components/actionbutton"
	"webtyp.com/dbsync"
	. "webtyp.com/dom"
	. "webtyp.com/html"
	"webtyp.com/lang"
	"webtyp.com/outbox"
	"webtyp.com/time"
	"webtyp.com/widget"
)

const (
	nameTray   = widget.Name("dbsynctray")
	partRow    = widget.Part("row")
	partReason = widget.Part("reason")
	partEmpty  = widget.Part("empty")
)

const (
	textDismiss     = lang.Text("Dismiss")
	textNoConflicts = lang.Text("No conflicts")
)

var (
	clsTray   = nameTray.Root()
	clsRow    = nameTray.Class(partRow)
	clsReason = nameTray.Class(partReason)
	clsEmpty  = nameTray.Class(partEmpty)
)

type tray struct {
	Element
	client *dbsync.Client
	mu     sync.Mutex // refresh runs on the sync goroutine and after a Dismiss
	// refused and failed are the data the rows are built from; rows is the
	// live list. Render builds a fresh signal from the data (as datatable does):
	// the signal owns the list's children, and the previous signal's elements
	// already belong to the painted list.
	refused []outbox.Mutation
	failed  error
	shown   int // Status.Rejected at the last refresh; -1 forces one
	rows    *SignalNodes
}

// Tray lists the writes the server refused, oldest first: when, the operation, and the server's
// reason, each with a Dismiss button (c.Dismiss). Empty → "No conflicts".
func Tray(c *dbsync.Client) Component {
	t := &tray{client: c, shown: -1, rows: NewNodes()}
	t.refresh(c.Status())
	c.OnStatus(t.refresh)
	return t
}

// refresh re-reads the refused writes when their count changed: the status
// changes on every queued write, the tray only when the server refuses one or
// the person dismisses one.
func (t *tray) refresh(s dbsync.Status) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.Rejected == t.shown {
		return
	}
	t.shown = s.Rejected
	t.refused, t.failed = t.client.Rejected()
	t.rows.Set(t.build())
}

func (t *tray) build() []*Element {
	if t.failed != nil {
		return []*Element{Div().Set(clsRow.AsAttr()).Text(t.failed.Error())}
	}
	if len(t.refused) == 0 {
		return []*Element{Div().Set(clsEmpty.AsAttr()).Text(lang.Translate(textNoConflicts).String())}
	}
	rows := make([]*Element, 0, len(t.refused))
	for _, m := range t.refused {
		rows = append(rows, t.row(m))
	}
	return rows
}

// row is one refused write. A Dismiss that fails shows its error in the row
// and leaves the write in the tray.
func (t *tray) row(m outbox.Mutation) *Element {
	failure := NewString("")
	dismiss := &actionbutton.ActionButton{
		Text:    textDismiss,
		Variant: actionbutton.Secondary,
		OnClick: func(Event) {
			if err := t.client.Dismiss(m.Id); err != nil {
				failure.Set(err.Error())
			}
		},
	}
	return Div().Set(clsRow.AsAttr()).Key(m.Id).
		Child(Span().Text(time.FormatDateTime(m.CreatedAt.Unix()))).
		Child(Span().Text(m.Op)).
		Child(Span().Set(clsReason.AsAttr()).Text(m.Reason)).
		Child(Span().BindText(failure)).
		Child(dismiss.Render())
}

func (t *tray) WidgetName() widget.Name { return nameTray }
func (t *tray) WidgetKind() widget.Kind { return widget.Region }

func (t *tray) Render() *Element {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = NewNodes(t.build()...)
	return Div().Set(clsTray.AsAttr()).BindChildren(t.rows)
}
