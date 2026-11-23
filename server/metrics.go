package server

import (
	"expvar"
	"log/slog"
	"net/http"
)

// Counter names, as they appear in /debug/vars.
const (
	idsIssued     = "ids_issued_total"
	clockBackward = "clock_backward_total"
	refusals      = "refusals_total"
	seqWaits      = "seq_overflow_waits_total"
)

// Metrics counts generator events with expvar and implements snowflake.Hooks.
// It keeps its own expvar.Map instead of the global registry, so there is no global state
// and two Metrics can exist side by side.
type Metrics struct {
	vars expvar.Map
	log  *slog.Logger
}

// NewMetrics returns Metrics with all four counters at zero, so they show up before the first event.
func NewMetrics(log *slog.Logger) *Metrics {
	m := &Metrics{log: log}
	for _, name := range []string{idsIssued, clockBackward, refusals, seqWaits} {
		m.vars.Add(name, 0)
	}
	return m
}

func (m *Metrics) OnIssued()  { m.vars.Add(idsIssued, 1) }
func (m *Metrics) OnRefused() { m.vars.Add(refusals, 1) }
func (m *Metrics) OnSeqWait() { m.vars.Add(seqWaits, 1) }

// OnClockBackward also logs, because a clock stepping back is something an operator should see.
func (m *Metrics) OnClockBackward() {
	m.vars.Add(clockBackward, 1)
	m.log.Warn("clock went backwards")
}

// ServeHTTP writes the counters as JSON, in the same shape as the standard /debug/vars page.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(m.vars.String()))
}
