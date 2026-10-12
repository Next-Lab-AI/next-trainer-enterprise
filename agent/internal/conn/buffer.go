package conn

import (
	"sync"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type SliceBuffer struct {
	mu     sync.Mutex
	frames []protocol.Frame
}

func (b *SliceBuffer) Append(f protocol.Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames = append(b.frames, f)
}

func (b *SliceBuffer) Replay() []protocol.Frame {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.frames
	b.frames = nil
	return out
}

func (b *SliceBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.frames)
}
