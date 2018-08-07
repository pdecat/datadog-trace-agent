package main

import (
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DataDog/datadog-trace-agent/info"
	"github.com/DataDog/datadog-trace-agent/model"
	"github.com/tinylib/msgp/msgp"
)

type collector struct {
	receiver *HTTPReceiver

	mu     sync.RWMutex
	size   int
	traces map[uint64]collectorItem
}

const maxCollectorSize = 200 * 1024 * 1024 // 200MB

func newCollector(r *HTTPReceiver) http.Handler {
	c := &collector{receiver: r}
	return c
}

type collectorItem struct {
	size  int
	spans model.Trace
}

func (c *collector) checkSize() {
	if c.size > maxCollectorSize {
		// evict oldest until we are under max size again.
		// log if a "young" trace is evicted.
	}
}

func (c *collector) Add(spans model.Trace) {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := time.Now()
	for _, span := range spans {
		v, ok := c.traces[span.TraceID]
		if !ok {
			c.traces[span.TraceID] = collectorItem{}
			v = c.traces[span.TraceID]
		}
		v.spans = append(v.spans, span)
		size := span.Msgsize()
		v.size += size
		c.size += size
		v.received = t
		// evict rules
		if span.ParentID == 0 {
			c.evict(span.TraceID)
		}
		c.checkSize()
	}
}

// must be guarded by caller
func (c *collector) evict(id uint64) {
	v, ok := c.traces[id]
	if !ok {
		return
	}
	c.receiver.traces <- v.spans
	c.size -= v.size
	delete(c.traces, id)
}

const maxRequestBodyLengthV1 = 10 * 1024 * 1024

func (c *collector) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req.Body = model.NewLimitedReader(req.Body, maxRequestBodyLengthV1)
	defer req.Body.Close()

	if !c.receiver.preSampler.Sample(req) {
		io.Copy(ioutil.Discard, req.Body)
		HTTPOK(w)
		return
	}

	var spans model.Trace // spans here are unrelated
	if err := msgp.Decode(req.Body, &spans); err != nil {
		HTTPDecodingError(err, []string{"handler:spans", "v1"}, w)
		return
	}
	HTTPOK(w)

	tags := info.Tags{
		Lang:          req.Header.Get("Datadog-Meta-Lang"),
		LangVersion:   req.Header.Get("Datadog-Meta-Lang-Version"),
		Interpreter:   req.Header.Get("Datadog-Meta-Lang-Interpreter"),
		TracerVersion: req.Header.Get("Datadog-Meta-Tracer-Version"),
	}
	ts := c.receiver.stats.GetTagStats(tags)
	bytesRead := req.Body.(*model.LimitedReader).Count
	if bytesRead > 0 {
		atomic.AddInt64(&ts.TracesBytes, int64(bytesRead))
	}
	c.Add(spans)
}
