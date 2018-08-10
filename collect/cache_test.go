package collect

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DataDog/datadog-trace-agent/model"
)

var (
	s11    = testSpan(1, 1, 0) // root
	s12    = testSpan(1, 2, 1) // child of s11
	s13    = testSpan(1, 3, 1) // child of s11
	trace1 = model.Trace{s11, s12, s13}
)

var (
	s21    = testSpan(2, 1, 0) // root
	s22    = testSpan(2, 2, 1) // child of s21
	s23    = testSpan(2, 3, 2) // child of s22
	trace2 = model.Trace{s21, s22, s23}
)

func TestCacheEvictReasonSpace(t *testing.T) {
	var evicted *EvictedTrace
	maxSize := s12.Msgsize() + s13.Msgsize() + s22.Msgsize()
	c := NewCache(func(et *EvictedTrace) { evicted = et }, maxSize)
	shouldHave := func(traces ...*trace) { cacheContains(t, c, traces...) }
	shouldEvict := func(want *EvictedTrace) { sameEvictedTrace(t, evicted, want) }

	t1 := time.Now()
	c.addWithTime([]*model.Span{s12, s13}, t1)
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s12.Msgsize() + s13.Msgsize(),
		lastmod: t1,
		spans:   model.Trace{s12, s13},
	})
	if evicted != nil {
		t.Fatal("unexpected evict")
	}

	// touch limit
	t2 := t1.Add(time.Second)
	c.addWithTime([]*model.Span{s22}, t2)
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s12.Msgsize() + s13.Msgsize(),
		lastmod: t1,
		spans:   model.Trace{s12, s13},
	}, &trace{
		key:     s21.TraceID,
		size:    s22.Msgsize(),
		lastmod: t2,
		spans:   model.Trace{s22},
	})
	if evicted != nil {
		t.Fatal("unexpected evict")
	}

	// go overboard on trace 2, trace 1 is oldest - gets evicted
	t3 := t1.Add(time.Second)
	c.addWithTime([]*model.Span{s23}, t3)
	shouldHave(&trace{
		key:     s21.TraceID,
		size:    s22.Msgsize() + s23.Msgsize(),
		lastmod: t3,
		spans:   model.Trace{s22, s23},
	})
	shouldEvict(&EvictedTrace{
		Reason: ReasonSpace,
		Root:   nil,
		Trace:  model.Trace{s12, s13},
	})
}

func TestCacheEvictReasonRoot(t *testing.T) {
	var evicted *EvictedTrace
	c := NewCache(func(et *EvictedTrace) { evicted = et }, 1000)
	shouldHave := func(traces ...*trace) { cacheContains(t, c, traces...) }
	shouldEvict := func(want *EvictedTrace) { sameEvictedTrace(t, evicted, want) }

	// add some children
	t1 := time.Now()
	c.addWithTime([]*model.Span{s13, s22, s23}, t1)
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s13.Msgsize(),
		lastmod: t1,
		spans:   model.Trace{s13},
	}, &trace{
		key:     s21.TraceID,
		size:    s22.Msgsize() + s23.Msgsize(),
		lastmod: t1,
		spans:   model.Trace{s22, s23},
	})
	if evicted != nil {
		t.Fatal("unexpected eviction")
	}

	// include root of trace 1
	t2 := t1.Add(time.Second)
	c.addWithTime([]*model.Span{s11, s12}, t2)
	shouldHave(&trace{
		key:     s21.TraceID,
		size:    s22.Msgsize() + s23.Msgsize(),
		lastmod: t1,
		spans:   model.Trace{s22, s23},
	})
	shouldEvict(&EvictedTrace{
		Reason: ReasonRoot,
		Root:   s11,
		Trace:  trace1,
	})
}

func TestCacheAddSpan(t *testing.T) {
	now := time.Now()
	sec := func(s time.Duration) time.Time {
		return now.Add(s)
	}
	c := NewCache(func(et *EvictedTrace) {}, 1000)
	shouldHave := func(traces ...*trace) {
		cacheContains(t, c, traces...)
	}

	// trace 1, span 1
	c.addSpan(s11, sec(1))
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s11.Msgsize(),
		lastmod: sec(1),
		spans:   model.Trace{s11},
	})

	// trace 1, span 2
	c.addSpan(s12, sec(2))
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s11.Msgsize() + s12.Msgsize(),
		lastmod: sec(2),
		spans:   model.Trace{s11, s12},
	})

	// trace 2, span 1
	c.addSpan(s21, sec(3))
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s11.Msgsize() + s12.Msgsize(),
		lastmod: sec(2),
		spans:   model.Trace{s11, s12},
	}, &trace{
		key:     s21.TraceID,
		size:    s21.Msgsize(),
		lastmod: sec(3),
		spans:   model.Trace{s21},
	})

	// trace 1, span 3 (list order should change)
	c.addSpan(s13, sec(1))
	shouldHave(&trace{
		key:     s21.TraceID,
		size:    s21.Msgsize(),
		lastmod: sec(3),
		spans:   model.Trace{s21},
	}, &trace{
		key:     s11.TraceID,
		size:    s11.Msgsize() + s12.Msgsize() + s13.Msgsize(),
		lastmod: sec(1),
		spans:   model.Trace{s11, s12, s13},
	})

	// trace 2, span 2 (list order should change again)
	c.addSpan(s22, sec(2))
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s11.Msgsize() + s12.Msgsize() + s13.Msgsize(),
		lastmod: sec(1),
		spans:   model.Trace{s11, s12, s13},
	}, &trace{
		key:     s21.TraceID,
		size:    s21.Msgsize() + s22.Msgsize(),
		lastmod: sec(2),
		spans:   model.Trace{s21, s22},
	})

	// trace 2, span 3
	c.addSpan(s23, sec(3))
	shouldHave(&trace{
		key:     s11.TraceID,
		size:    s11.Msgsize() + s12.Msgsize() + s13.Msgsize(),
		lastmod: sec(1),
		spans:   model.Trace{s11, s12, s13},
	}, &trace{
		key:     s21.TraceID,
		size:    s21.Msgsize() + s22.Msgsize() + s23.Msgsize(),
		lastmod: sec(3),
		spans:   model.Trace{s21, s22, s23},
	})
}

// cacheContains tests that exactly these traces exist in the cache,
// in the same order as provided, oldest to newest.
func cacheContains(t *testing.T, c *Cache, traces ...*trace) {
	if len(traces) != c.Len() {
		t.Fatalf("wanted %d traces, got %d", len(traces), c.Len())
	}
	iter := c.newReverseIterator()
	if len(traces) != iter.len() {
		t.Fatalf("want %d list elements, got %d", len(traces), iter.len())
	}
	var totalSize int
	for _, tr := range traces {
		itr, ok := iter.getAndAdvance()
		if !ok {
			t.Fatalf("trace %d missing from list", tr.key)
		}
		if !reflect.DeepEqual(tr, itr) {
			t.Fatalf("bad list order: want %d, got %d", tr.key, itr.key)
		}
		got, ok := c.get(tr.key)
		if !ok {
			t.Fatalf("did not create trace %d", tr.key)
		}
		if got.key != tr.key {
			t.Fatalf("expected key %d, got %d", tr.key, got.key)
		}
		if got.size != tr.size {
			t.Fatalf("expected size %d, got %d", tr.size, got.size)
		}
		if !tr.lastmod.Equal(got.lastmod) {
			t.Fatalf("wanted time %q, got %q", tr.lastmod, got.lastmod)
		}
		if !reflect.DeepEqual(got.spans, tr.spans) {
			t.Fatalf("wanted spans:\n%+v\n--- got:\n%+v", tr.spans, got.spans)
		}
		totalSize += tr.size
	}
	if c.size != totalSize {
		t.Fatal("size mismatch")
	}
}

// BenchmarkCacheAddSpan benchmarks the speed at which we can add one span
// into the cache.
func BenchmarkCacheAddSpan(b *testing.B) {
	now := time.Now()
	maxTraces := 10 // max number of traces to put spans into

	for _, max := range []int{
		10,    // few traces, testing load on the list move
		10000, // many traces, testing load on the list push
	} {
		b.Run(fmt.Sprintf("%d-traces", max), func(b *testing.B) {
			// we can use maxSize 1; addSpan doesn't care
			c := NewCache(func(_ *EvictedTrace) {}, 1)
			b.SetBytes(int64(testSpan(0, 0, 0).Msgsize()))
			var traceID, spanID uint64
			for i := 0; i < b.N; i++ {
				// generate a random span for one of the traces
				traceID++
				spanID++
				span := testSpan(traceID%uint64(maxTraces+1), spanID, 0)

				c.addSpan(span, now)
			}
		})
	}
}

func sameEvictedTrace(t *testing.T, got, want *EvictedTrace) {
	if got == nil {
		t.Fatal("got nil")
	}
	if got.Reason != want.Reason {
		t.Fatalf("wanted reason %d got %d", want.Reason, got.Reason)
	}
	if !reflect.DeepEqual(got.Root, want.Root) {
		t.Fatal("not same root")
	}
	if len(got.Trace) != len(want.Trace) {
		t.Fatal("length mismatch")
	}
	for _, s1 := range got.Trace {
		var found bool
		for _, s2 := range want.Trace {
			if s1 == s2 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("span %s not found in %d", s1.Name, s1.TraceID)
		}
	}
}

func testSpan(traceID, spanID, parentID uint64) *model.Span {
	now := time.Now()
	span := &model.Span{
		TraceID:  traceID,
		SpanID:   spanID,
		ParentID: parentID,
		Duration: int64(time.Second),
		Start:    now.UnixNano(),
		Service:  "service",
		Name:     fmt.Sprintf("%d.%d.%d", traceID, spanID, parentID),
		Resource: "resource",
	}
	return span
}
