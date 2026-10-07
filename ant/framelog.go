package ant

import (
	"bufio"
	"fmt"
	"io"
	"sync"
	"time"
)

// FrameLog writes raw frames as "<seconds since start> <rx|tx> <hex>" lines,
// timed with the monotonic clock. Its Trace method fits Options.Trace.
type FrameLog struct {
	mu    sync.Mutex
	w     *bufio.Writer
	start time.Time
}

func NewFrameLog(w io.Writer) *FrameLog {
	l := &FrameLog{w: bufio.NewWriter(w), start: time.Now()}
	fmt.Fprintf(l.w, "# ANT raw frame log started %s\n", l.start.Format(time.RFC3339Nano))
	return l
}

func (l *FrameLog) Trace(dir Direction, frame []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%.6f %s % X\n", time.Since(l.start).Seconds(), dir, frame)
}

func (l *FrameLog) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Flush()
}
