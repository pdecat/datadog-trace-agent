package main

import (
	"io"
	"io/ioutil"
	"net/http"
	"sync/atomic"

	"github.com/DataDog/datadog-trace-agent/info"
	"github.com/DataDog/datadog-trace-agent/model"
	"github.com/tinylib/msgp/msgp"
)

type collector struct {
	receiver *HTTPReceiver
}

func newCollector(r *HTTPReceiver) http.Handler {
	c := &collector{receiver: r}
	return c
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
	// add to cache
}
