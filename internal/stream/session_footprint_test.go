package stream

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// payloadOf returns a JSON value whose encoding is exactly size bytes.
func payloadOf(size int) json.RawMessage {
	if size < 2 {
		return json.RawMessage(strings.Repeat("0", size))
	}
	return json.RawMessage(`"` + strings.Repeat("x", size-2) + `"`)
}
func TestRingStaysWithinBoundsUnderLargePayloads(t *testing.T) {
	for _, c := range []struct{ size, kept int }{{8 << 10, 1024}, {16 << 10, 512}} {
		s := New("session")
		payload := payloadOf(c.size)
		appends := 2*c.kept + 7
		for range appends {
			s.Append("message.updated", payload)
		}
		s.mu.Lock()
		total := 0
		for _, e := range s.ring {
			total += len(e.Payload)
		}
		first, last := s.ring[0].Cursor.Sequence, s.ring[len(s.ring)-1].Cursor.Sequence
		n, bytes := len(s.ring), s.bytes
		s.mu.Unlock()
		if n != c.kept || total != bytes || bytes > 8<<20 || last != uint64(appends) || first != uint64(appends-c.kept+1) {
			t.Fatalf("payload %d: kept %d events (%d..%d), %d bytes counted, %d held", c.size, n, first, last, bytes, total)
		}
	}
}

// ringDriver follows the ring's backing array. Trimming with ring[1:] leaves
// the leading slots in the array until append moves the ring to a new one;
// Commit empties them, so they hold an Event struct but no payload.
type ringDriver struct {
	s        *Session
	arrayCap int
	prevCap  int
	appends  int
	steady   bool
}

func (d *ringDriver) append(payload json.RawMessage) {
	before := len(d.s.ring)
	d.s.Append("message.updated", payload)
	d.appends++
	if c := cap(d.s.ring); c > d.prevCap {
		trimmed := before + 1 - len(d.s.ring)
		d.arrayCap = c + trimmed
		d.steady = d.steady || trimmed > 0
	}
	d.prevCap = cap(d.s.ring)
}
func (d *ringDriver) slots() int { return d.arrayCap - cap(d.s.ring) + len(d.s.ring) }
func (d *ringDriver) full() bool { return len(d.s.ring) == 1024 }

// atArrayStart and atArrayEnd bracket one backing array's life once the ring
// is full: right after append reallocated, and just before it will.
func (d *ringDriver) atArrayStart() bool { return d.steady && d.slots() == len(d.s.ring)+1 }
func (d *ringDriver) atArrayEnd() bool   { return d.steady && cap(d.s.ring) == len(d.s.ring) }
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestFullRingHeapStaysNearLimit(t *testing.T) {
	const limit = 8<<20 + 512<<10
	payload := payloadOf(8 << 10)
	base := liveHeap()
	d := &ringDriver{s: New("claude:00000000-0000-4000-8000-00000000000a")}
	var starts, ends int
	for d.appends < 4000 {
		d.append(payload)
		start, end := d.atArrayStart(), d.atArrayEnd()
		if !d.full() || !start && !end {
			continue
		}
		if heap := liveHeap() - base; heap > limit {
			t.Fatalf("after %d appends the ring holds %d bytes of heap across %d slots, limit %d", d.appends, heap, d.slots(), limit)
		}
		if start {
			starts++
		}
		if end {
			ends++
		}
	}
	runtime.KeepAlive(d.s)
	if starts == 0 || ends == 0 {
		t.Fatalf("sampled %d array starts and %d array ends", starts, ends)
	}
}

// BenchmarkRingFootprint reports the live heap of one full ring. Each
// measurement is one pass; run it with -benchtime 1x.
func BenchmarkRingFootprint(b *testing.B) {
	for _, size := range []int{8 << 10, 1} {
		b.Run(fmt.Sprintf("payload=%dB", size), func(b *testing.B) {
			for range b.N {
				measureRing(b, size)
			}
		})
	}
}
func measureRing(b *testing.B, size int) {
	payload := payloadOf(size)
	base := liveHeap()
	d := &ringDriver{s: New("claude:00000000-0000-4000-8000-00000000000a")}
	var minHeap, maxHeap uint64
	var minSlots, maxSlots, samples int
	for d.appends < 6000 {
		d.append(payload)
		if !d.full() || !d.atArrayStart() && !d.atArrayEnd() {
			continue
		}
		heap := liveHeap() - base
		if samples == 0 || heap < minHeap {
			minHeap, minSlots = heap, d.slots()
		}
		if heap > maxHeap {
			maxHeap, maxSlots = heap, d.slots()
		}
		samples++
	}
	runtime.KeepAlive(d.s)
	mib := float64(1 << 20)
	b.ReportMetric(float64(minHeap)/mib, "min-MiB")
	b.ReportMetric(float64(maxHeap)/mib, "max-MiB")
	b.ReportMetric(float64(minSlots), "min-slots")
	b.ReportMetric(float64(maxSlots), "max-slots")
	b.ReportMetric(float64(maxHeap)/float64(len(d.s.ring))-float64(len(payload)), "overhead-B/event")
	b.ReportMetric(float64(unsafe.Sizeof(Event{})), "Event-B")
	b.ReportMetric(float64(samples), "samples")
}

// BenchmarkRingFootprint36Sessions is the registry's worst case: maxInactive
// (32) ended items and 4 active ones, every ring full of 8 KiB events.
func BenchmarkRingFootprint36Sessions(b *testing.B) {
	for range b.N {
		payload := payloadOf(8 << 10)
		base := liveHeap()
		drivers := make([]*ringDriver, 36)
		for i := range drivers {
			drivers[i] = &ringDriver{s: New(fmt.Sprintf("claude:%08x-0000-4000-8000-00000000000a", i))}
			for d := drivers[i]; !d.full() || !d.atArrayStart(); {
				d.append(payload)
			}
		}
		low := liveHeap() - base
		for _, d := range drivers {
			for !d.atArrayEnd() {
				d.append(payload)
			}
		}
		high := liveHeap() - base
		runtime.KeepAlive(drivers)
		mib := float64(1 << 20)
		b.ReportMetric(float64(low)/mib, "array-start-MiB")
		b.ReportMetric(float64(high)/mib, "array-end-MiB")
		b.ReportMetric(float64(drivers[0].slots()), "slots/session")
	}
}
