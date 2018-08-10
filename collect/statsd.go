package collect

import (
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

// monitor runs a loop, sending occasional statsd entries.
func (c *Cache) monitor(client *statsd.Client) {
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
			c.sendSizeStats(client)
		}
	}
}

var statsdTags = []string{"version:v1"}

func (c *Cache) sendAgeStats(client *statsd.Client) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	iter := c.newReverseIterator()
	now := time.Now()
	for {
		t, ok := iter.getAndAdvance()
		if !ok {
			return
		}
		age := now.Sub(t.lastmod)
		client.Histogram("trace_age", float64(age), statsdTags, 1)
	}
}

func (c *Cache) sendSizeStats(client *statsd.Client) {
	client.Gauge("len", float64(c.newIterator().len()), statsdTags, 1)
	client.Gauge("bytes", float64(c.size), statsdTags, 1)
}
