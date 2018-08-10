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

var statsdTags = []string{"version:v1"}

func (c *Cache) computeStats(client statsd.StatsClient) {
	iter := c.newReverseIterator()
	now := time.Now()
	// age distribution counters
	var (
		n1mTo5m  float64 // age 1m-5m
		n5mTo10m float64 // age 5m-10m
		nOver10m float64 // age >10m
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
			n1mTo5m++
		case age < 10*time.Minute:
			n5mTo10m++
		default:
			nOver10m++
		}
	}

	total := float64(iter.len())
	client.Gauge("datadog.trace_agent.cache.ages.under_1m", total-n1mTo5m-n5mTo10m-nOver10m, statsdTags, 1)
	client.Gauge("datadog.trace_agent.cache.ages.1m_to_5m", n1mTo5m, statsdTags, 1)
	client.Gauge("datadog.trace_agent.cache.ages.5m_to_10m", n5mTo10m, statsdTags, 1)
	client.Gauge("datadog.trace_agent.cache.ages.over_10m", nOver10m, statsdTags, 1)
	client.Gauge("datadog.trace_agent.cache.count", total, statsdTags, 1)

	c.mu.RLock()
	client.Gauge("datadog.trace_agent.cache.bytes", float64(c.size), statsdTags, 1)
	c.mu.RUnlock()
}
