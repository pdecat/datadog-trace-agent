package collect

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

// NewCache returns a new Cache which will call the given function when a trace
// is evicted due to completion or due to maxSize being reached.
func NewCache(onEvict func(*EvictedTrace), maxSize int) *Cache {
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	return &Cache{
		onEvict: onEvict,
		maxSize: int(maxSize),
		ll:      list.New(),
		cache:   make(map[uint64]*list.Element),
	}
}

// Cache caches spans until they are considered complete based on certain rules,
// or until they are evicted due to memory consumption limits (maxSize).
type Cache struct {
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
func (c *Cache) Add(spans []*model.Span) {
	c.addWithTime(spans, time.Now())
}

func (c *Cache) addWithTime(spans []*model.Span, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var roots []*model.Span
	for _, span := range spans {
		c.addSpan(span, now)
		if isRoot(span) {
			roots = append(roots, span)
		}
	}
	for _, span := range roots {
		c.evictReasonRoot(span)
	}
	for c.size > c.maxSize {
		c.evictReasonSpace()
	}
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

func (c *Cache) addSpan(span *model.Span, now time.Time) {
	key := span.TraceID
	ee, ok := c.cache[key]
	if ok {
		// trace already started
		c.ll.MoveToFront(ee)
	} else {
		// this is a new trace
		ee = c.ll.PushFront(&trace{key: key})
		c.cache[key] = ee
	}
	size := span.Msgsize()
	trace := ee.Value.(*trace)
	trace.spans = append(trace.spans, span)
	trace.lastmod = now
	trace.size += size
	c.size += size
}

// evictReasonSpace evicts the least recently added to trace from the cache.
func (c *Cache) evictReasonSpace() {
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
}

// evictReasonRoot evicts the trace at key with the given root.
func (c *Cache) evictReasonRoot(root *model.Span) {
	key := root.TraceID
	if ele, found := c.cache[key]; found {
		c.onEvict(&EvictedTrace{
			Reason: ReasonRoot,
			Trace:  model.Trace(ele.Value.(*trace).spans),
			Root:   root,
		})
		c.remove(ele)
	}
}

func (c *Cache) get(key uint64) (t *trace, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ele, ok := c.cache[key]
	if ok {
		return ele.Value.(*trace), true
	}
	return nil, false
}

// remove removes ele from the cache.
func (c *Cache) remove(ele *list.Element) {
	trace := ele.Value.(*trace)
	c.size -= trace.size
	c.ll.Remove(ele)
	delete(c.cache, trace.key)
}

// isRoot returns true if the span is considered to be the last in its trace.
func isRoot(span *model.Span) bool {
	rule1 := span.ParentID == 0                                    // parent ID is 0, means root
	rule2 := span.Metrics != nil && span.Metrics[tagRootSpan] == 1 // client set root
	return rule1 || rule2
}

// iterator is an iterator through the list. The iterator points to a nil
// element at the end of the list.
type iterator struct {
	c       *Cache
	e       *list.Element
	forward bool
}

// newIterator returns a new iterator for the Cache.
func (c *Cache) newIterator() *iterator {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &iterator{c: c, e: c.ll.Front(), forward: true}
}

// newReverseIterator returns a new reverse iterator for the Cache.
func (c *Cache) newReverseIterator() *iterator {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &iterator{c: c, e: c.ll.Back(), forward: false}
}

// len returns the total number of items in the list.
func (i *iterator) len() int {
	i.c.mu.RLock()
	defer i.c.mu.RUnlock()
	return i.c.ll.Len()
}

// getAndAdvance returns key, value, true if the current entry is valid and advances the
// iterator. Otherwise it returns nil, nil, false.
func (i *iterator) getAndAdvance() (t *trace, ok bool) {
	i.c.mu.Lock()
	defer i.c.mu.Unlock()
	if i.e == nil {
		return nil, false
	}
	t = i.e.Value.(*trace)
	if i.forward {
		i.e = i.e.Next()
	} else {
		i.e = i.e.Prev()
	}
	return t, true
}
