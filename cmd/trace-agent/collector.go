package main

import (
	"io"
	"io/ioutil"
	"net/http"
	"sync/atomic"

	"github.com/DataDog/datadog-trace-agent/collect"
	"github.com/DataDog/datadog-trace-agent/info"
	"github.com/DataDog/datadog-trace-agent/model"
	"github.com/DataDog/datadog-trace-agent/statsd"

	"github.com/tinylib/msgp/msgp"
)

type collector struct {
	receiver *HTTPReceiver
	cache    *collect.Cache
	out      chan collect.EvictedTrace
}

const (
	maxRequestBodyLengthV1 = 10 * 1024 * 1024  // 10MB
	maxCacheSize           = 200 * 1024 * 1024 // 200MB
)

func newCollector(r *HTTPReceiver) http.Handler {
	c := &collector{
		receiver: r,
		out:      make(chan collect.EvictedTrace, 1000),
	}
	c.cache = collect.NewCache(collect.Settings{
		Out:     c.out,
		MaxSize: maxCacheSize,
		Statsd:  statsd.Client,
	})
	go c.waitForTraces()
	return c
}

func (c *collector) waitForTraces() {
	for et := range c.out {
		c.handleEvictedTrace(&et)
	}
}

func (c *collector) handleEvictedTrace(et *collect.EvictedTrace) {
	var service, name string
	if n := len(et.Trace); n >= 0 {
		// Log the service and the name of the last span in the trace.
		// It's the one that finished last.
		service = et.Trace[n-1].Service
		name = et.Trace[n-1].Name
	}
	switch et.Reason {
	case collect.ReasonSpace:
		statsd.Client.Count("datadog.trace_agent.cache.evicted", 1, []string{
			"version:v1",
			"reason:space",
			"service:" + service,
			"name:" + name,
		}, 1)

	case collect.ReasonRoot:
		statsd.Client.Count("datadog.trace_agent.cache.evicted", 1, []string{
			"version:v1",
			"reason:root",
			"service:" + service,
			"name:" + name,
		}, 1)
	}
	c.receiver.traces <- et.Trace
}

func (c *collector) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req.Body = model.NewLimitedReader(req.Body, maxRequestBodyLengthV1)
	defer req.Body.Close()

	// TODO: get count from msgpack array header (not HTTP header) for presample.
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

	c.cache.Add(spans)
}
