// Package collect provides tools to buffer spans in a size bounded cache
// until they are grown into complete traces or evicted for other reasons.
package collect

import (
	"time"

	"github.com/DataDog/datadog-trace-agent/model"
)

// EvictionReason specifies the reason why a trace was evicted.
type EvictionReason int

const (
	// ReasonRoot specifies that a trace was evicted because the root was received.
	ReasonRoot EvictionReason = iota
	// ReasonSpace specifies that this trace had to be evicted to free up memory space.
	ReasonSpace
)

// EvictedTrace contains information about a trace which was evicted.
type EvictedTrace struct {
	// Reason specifies the reason why this trace was evicted.
	Reason EvictionReason
	// Root specifies the root which was selected for this trace when the
	// reason is ReasonRoot.
	Root *model.Span
	// Trace holds the trace that was evicted. It is only available
	// for the duration of the OnEvict call.
	Trace model.Trace
	// LastMod specifies the time when the last span was added to this
	// trace.
	LastMod time.Time
}
