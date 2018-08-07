package waitlist

import (
	"testing"
	"time"

	"github.com/DataDog/datadog-trace-agent/model"
)

var nextID uint64 = 0

func testSpan(name string, traceID, parentID uint64, remoteParent bool) *model.Span {
	now := time.Now()
	nextID++
	span := &model.Span{
		SpanID:   nextID,
		TraceID:  traceID,
		ParentID: parentID,
		Duration: int64(time.Second),
		Start:    now.UnixNano(),
		Service:  "service",
		Name:     name,
		Resource: "resource",
	}
	if remoteParent {
		span.Metrics = map[string]float64{tagRootSpan: 1}
	}
	return span
}

func TestCache(t *testing.T) {
}
