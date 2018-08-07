// Package waitlist provides tools to buffer spans in a size bounded cache
// until they are grown into complete traces or evicted for other reasons.
package waitlist

import "github.com/DataDog/datadog-trace-agent/model"

// Adder implementations are expected to manage a list of spans until they
// mature into complete traces.
type Adder interface {
	// Adder adds a new span to list.
	Add([]*model.Span)
}

// EvictionReason specifies the reason why a trace was evicted.
type EvictionReason int

const (
	// ReasonComplete specifies that a trace was evicted because it was completed.
	ReasonComplete EvictionReason = iota
	// ReasonSpace specifies that this trace had to be evicted to free up memory space.
	ReasonSpace
)

// EvictedTrace contains information about a trace which was evicted.
type EvictedTrace struct {
	// Reason specifies the reason why this trace was evicted.
	Reason EvictionReason
	// Root, when set, specifies the root which was selected for this trace.
	Root *model.Span
	// Trace holds the trace that was evicted. It is only available
	// for the duration of the OnEvict call.
	Trace model.Trace
}

// New returns a new Adder which will call the given function when a trace
// is evicted due to completion or internal buffer space limitations.
func New(onEvict func(*EvictedTrace)) Adder {
	return newCache(onEvict, defaultMaxSize)
}
