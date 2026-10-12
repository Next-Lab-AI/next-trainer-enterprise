package jobs

import (
	"sync"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type RingBuffer struct {
	mu       sync.Mutex
	maxBytes int
	frames   []protocol.Frame
	sizes    []int
	bytes    int
	gapFrom  int64
	gapTo    int64
	hasGap   bool
}

func NewRingBuffer(maxBytes int) *RingBuffer {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	return &RingBuffer{maxBytes: maxBytes}
}

func frameSize(f protocol.Frame) int {
	return len(f.Payload) + len(f.Type) + len(f.CommandID) + 64
}

func (b *RingBuffer) Append(f protocol.Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	size := frameSize(f)
	for len(b.frames) > 0 && b.bytes+size > b.maxBytes {
		dropped := b.frames[0]
		b.frames = b.frames[1:]
		b.bytes -= b.sizes[0]
		b.sizes = b.sizes[1:]
		if !b.hasGap {
			b.gapFrom = dropped.Seq
		}
		b.gapTo = dropped.Seq
		b.hasGap = true
	}
	b.frames = append(b.frames, f)
	b.sizes = append(b.sizes, size)
	b.bytes += size
}

func (b *RingBuffer) Replay() []protocol.Frame {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.frames
	b.frames = nil
	b.sizes = nil
	b.bytes = 0
	return out
}

func (b *RingBuffer) TakeGap() (from, to int64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	from, to, ok = b.gapFrom, b.gapTo, b.hasGap
	b.gapFrom, b.gapTo, b.hasGap = 0, 0, false
	return
}

func (b *RingBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.frames)
}
