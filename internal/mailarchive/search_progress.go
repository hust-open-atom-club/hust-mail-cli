package mailarchive

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Progress and per-message diagnostics may write from different goroutines.
type searchLog struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *searchLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
}

type searchProgress struct {
	out                               io.Writer
	started                           time.Time
	stop, done                        chan struct{}
	mu                                sync.Mutex
	scanned, matched, skipped, cached int
	emitted                           bool
}

func newSearchProgress(out io.Writer, quiet bool) *searchProgress {
	if quiet || out == io.Discard {
		return nil
	}
	p := &searchProgress{out: out, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	go p.run(5 * time.Second)
	return p
}

func (p *searchProgress) update(scanned, matched, skipped, cached int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.scanned, p.matched, p.skipped, p.cached = scanned, matched, skipped, cached
	p.mu.Unlock()
}

func (p *searchProgress) run(interval time.Duration) {
	defer close(p.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.mu.Lock()
			p.emitted = true
			line := fmt.Sprintf("Search progress: scanned=%d matched=%d skipped=%d cache_hits=%d elapsed=%s\n", p.scanned, p.matched, p.skipped, p.cached, time.Since(p.started).Truncate(time.Second))
			p.mu.Unlock()
			fmt.Fprint(p.out, line)
		}
	}
}

func (p *searchProgress) hasOutput() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.emitted
}

func (p *searchProgress) close() {
	if p == nil {
		return
	}
	close(p.stop)
	<-p.done
}
