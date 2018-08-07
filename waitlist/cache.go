package waitlist

import (
	"container/list"
	"sync"
	"time"

	"github.com/DataDog/datadog-trace-agent/model"
)

const tagRootSpan = "_root_span"

// defaultMaxSize holds the maximum size allowed for the cache.
//
// model.Span size is computed based on it's msgpack generated Msgsize [1] method. This
// is not an accurate representation of the space this span uses in memory, but it is a
// reliable approximation. We should consider the maximum space used by this package to
// be at worst double the value of this constant.
// [1] model/span_gen.go#333
const defaultMaxSize = 200 * 1024 * 1024 // 200MB

func newCache(onEvict func(*EvictedTrace), maxSize uint64) *cache {
	return &cache{
		onEvict: onEvict,
		maxSize: int(maxSize),
		ll:      list.New(),
		cache:   make(map[uint64]*list.Element),
	}
}

// cache caches spans until they are considered complete based on certain rules,
// or until they are evicted due to memory consumption limits (maxSize).
type cache struct {
	onEvict func(*EvictedTrace)
	maxSize int

	mu    sync.RWMutex
	ll    *list.List // traces ordered by age
	cache map[uint64]*list.Element
	size  int
}

type trace struct {
	key     uint64
	size    int
	lastmod time.Time
	spans   model.Trace
}

// Add adds a new list of spans to the cache and evicts them when they are completed
// or when they start filling up too much space.
func (c *cache) Add(spans []*model.Span) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for _, span := range spans {
		c.addSpan(span, now)
	}
	for c.size > c.maxSize {
		// continue reducing size until we are again below
		c.evictOldest()
	}
}

func (c *cache) addSpan(span *model.Span, lastmod time.Time) {
	key := span.TraceID
	ee, ok := c.cache[key]
	if ok {
		// trace already started
		c.ll.MoveToFront(ee)
	} else {
		// this is a new trace
		ee = c.ll.PushFront(&trace{
			key:     key,
			lastmod: lastmod,
			spans:   model.Trace{span},
		})
		c.cache[key] = ee
	}

	size := span.Msgsize()
	trace := ee.Value.(*trace)
	trace.spans = append(trace.spans, span)
	trace.lastmod = lastmod
	trace.size += size
	c.size += size

	if c.isLast(span) {
		// this span completes its trace
		c.evictWithRoot(key, span)
	}
}

// isLast returns true if the span is considered to be the last in its trace.
func (c *cache) isLast(span *model.Span) bool {
	rule1 := span.ParentID == 0                                    // parent ID is 0, means root
	rule2 := span.Metrics != nil && span.Metrics[tagRootSpan] == 1 // client set root
	return rule1 || rule2
}

// evictOldest evicts the least recently added to trace from the cache.
func (c *cache) evictOldest() {
	ele := c.ll.Back()
	if ele == nil {
		return
	}
	c.onEvict(&EvictedTrace{
		Reason: ReasonSpace,
		Trace:  model.Trace(ele.Value.(*trace).spans),
		// we don't have a root here, it can be calculated later.
	})
	c.remove(ele)
	return
}

// evictWithRoot evicts the trace at key with the given root.
func (c *cache) evictWithRoot(key uint64, root *model.Span) {
	if ele, found := c.cache[key]; found {
		c.onEvict(&EvictedTrace{
			Reason: ReasonComplete,
			Trace:  model.Trace(ele.Value.(*trace).spans),
			Root:   root,
		})
		c.remove(ele)
	}
	return
}

func (c *cache) get(key uint64) (t *trace, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ele, ok := c.cache[key]
	if ok {
		return ele.Value.(*trace), true
	}
	return nil, false
}

// remove removes ele from the cache.
func (c *cache) remove(ele *list.Element) {
	trace := ele.Value.(*trace)
	c.size -= trace.size
	c.ll.Remove(ele)
	delete(c.cache, trace.key)
}
