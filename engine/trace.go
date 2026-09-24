package engine

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
)

// TraceEntry is one line of a call's SIP ladder.
type TraceEntry struct {
	At   time.Time
	Out  bool   // sent by us
	Line string // start line or a note
	Raw  string // full message, only with Options.TraceBodies
}

type tracer struct {
	mu      sync.Mutex
	full    bool
	entries []TraceEntry
}

func (t *tracer) msg(out bool, m sip.Message) {
	e := TraceEntry{At: time.Now(), Out: out}
	switch m := m.(type) {
	case *sip.Request:
		e.Line = m.StartLine()
	case *sip.Response:
		e.Line = m.StartLine()
	}
	if t.full {
		e.Raw = m.String()
	}
	t.mu.Lock()
	t.entries = append(t.entries, e)
	t.mu.Unlock()
}

func (t *tracer) note(out bool, format string, args ...any) {
	e := TraceEntry{At: time.Now(), Out: out, Line: fmt.Sprintf(format, args...)}
	t.mu.Lock()
	t.entries = append(t.entries, e)
	t.mu.Unlock()
}

func (t *tracer) snapshot() []TraceEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]TraceEntry(nil), t.entries...)
}

// FormatLadder renders trace entries relative to the first one:
//
//	+0.000s  -> INVITE sip:702@pbx SIP/2.0
//	+0.012s  <- SIP/2.0 180 Ringing
func FormatLadder(entries []TraceEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	t0 := entries[0].At
	for _, e := range entries {
		dir := "<-"
		if e.Out {
			dir = "->"
		}
		fmt.Fprintf(&b, "+%.3fs  %s %s\n", e.At.Sub(t0).Seconds(), dir, e.Line)
		if e.Raw != "" {
			b.WriteString(e.Raw)
			b.WriteString("\n")
		}
	}
	return b.String()
}
