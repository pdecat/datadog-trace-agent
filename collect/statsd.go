package collect

import (
	"time"

	"github.com/DataDog/datadog-trace-agent/statsd"
)

// monitor runs a loop, sending occasional statsd entries.
func (c *Cache) monitor(client statsd.StatsClient) {
	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			c.computeStats(client)
		}
	}
}

func (c *Cache) computeStats(client statsd.StatsClient) {
	iter := c.newReverseIterator()
	now := time.Now()
	// age distribution counters
	var (
		n5m    float64 // age 1m-5m
		n10m   float64 // age 5m-10m
		nLarge float64 // age >10m
	)
	for {
		t, ok := iter.getAndAdvance()
		if !ok {
			return
		}
		age := now.Sub(t.lastmod)
		switch {
		case age <= time.Minute:
			break // stop counting, these are recent spans
		case age < 5*time.Minute:
			n5m++
		case age < 10*time.Minute:
			n10m++
		default:
			nLarge++
		}
	}

	total := float64(iter.len())
	client.Gauge("datadog.trace_agent.cache.traces", total-n5m-n10m-nLarge, []string{
		"version:v1",
		"category:under1mb",
	}, 1)

	client.Gauge("datadog.trace_agent.cache.traces", n5m, []string{
		"version:v1",
		"category:under5mb",
	}, 1)

	client.Gauge("datadog.trace_agent.cache.traces", n10m, []string{
		"version:v1",
		"category:under10mb",
	}, 1)

	client.Gauge("datadog.trace_agent.cache.traces", nLarge, []string{
		"version:v1",
		"category:over10mb",
	}, 1)

	c.mu.RLock()
	client.Gauge("datadog.trace_agent.cache.bytes", float64(c.size), []string{"version:v1"}, 1)
	c.mu.RUnlock()
}
