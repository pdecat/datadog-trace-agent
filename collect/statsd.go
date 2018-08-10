package collect

import (
	"time"

	"github.com/DataDog/datadog-trace-agent/statsd"
)

// monitor runs a loop, sending occasional statsd entries.
func (c *Cache) monitor(client statsd.StatsClient) {
	// send age distribution stats every 5 minutes
	ageTick := time.NewTicker(5 * time.Minute)
	defer ageTick.Stop()

	// send size stats every minute
	sizeTick := time.NewTicker(time.Minute)
	defer sizeTick.Stop()

	for {
		select {
		case <-ageTick.C:
			c.sendAgeStats(client)
		case <-sizeTick.C:
			c.mu.RLock()
			client.Gauge("datadog.trace_agent.cache.len", float64(c.newIterator().len()), statsdTags, 1)
			client.Gauge("datadog.trace_agent.cache.bytes", float64(c.size), statsdTags, 1)
			c.mu.RUnlock()
		}
	}
}

var statsdTags = []string{"version:v1"}

func (c *Cache) sendAgeStats(client statsd.StatsClient) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	iter := c.newReverseIterator()
	now := time.Now()
	for {
		t, ok := iter.getAndAdvance()
		if !ok {
			return
		}
		age := now.Sub(t.lastmod) / time.Second
		client.Histogram("datadog.trace_agent.cache.age_seconds", float64(age), statsdTags, 1)
	}
}
