package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync/atomic"

	"github.com/DataDog/datadog-trace-agent/collect"
	"github.com/DataDog/datadog-trace-agent/info"
	"github.com/DataDog/datadog-trace-agent/model"

	"github.com/tinylib/msgp/msgp"
)

type collector struct {
	receiver *HTTPReceiver
	cache    *collect.Cache
}

const maxRequestBodyLengthV1 = 10 * 1024 * 1024
const maxCacheSize = 200 * 1024 * 1024 // 200MB

func newCollector(r *HTTPReceiver) http.Handler {
	c := &collector{
		receiver: r,
	}
	var addr string
	if host := r.conf.StatsdHost; host != "" {
		addr = host
	}
	if port := r.conf.StatsdPort; port != 0 {
		addr += fmt.Sprintf(":%d", port)
	}
	c.cache = collect.NewCache(c.onEvict, maxCacheSize, addr)
	return c
}

func (c *collector) onEvict(et *collect.EvictedTrace) {
	switch et.Reason {
	case collect.ReasonSpace:
		// stat, count evicted with no root
	case collect.ReasonRoot:
		// stat, count completed
	}
	// TODO
	// c.r.traces <- et
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
	// add to cache
}
