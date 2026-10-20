package mailarchive

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestSearchProgressWhileParsing(t *testing.T) {
	events := &progressEvents{events: make(chan string, 10)}
	p := &searchProgress{out: &searchLog{out: events}, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	p.update(10, 2, 1, 7)
	go p.run(10 * time.Millisecond)
	defer p.close()
	select {
	case text := <-events.events:
		if !strings.Contains(text, "Search progress: scanned=10 matched=2 skipped=1 cache_hits=7") || strings.Contains(text, "\033") {
			t.Fatal("invalid redirected progress", text)
		}
	case <-time.After(time.Second):
		t.Fatal("search stayed silent while parsing")
	}
	if !p.hasOutput() {
		t.Fatal("missing final-summary indicator")
	}
}

func TestSearchProgressQuiet(t *testing.T) {
	p := newSearchProgress(io.Discard, true)
	if p != nil {
		t.Fatal("quiet mode started progress")
	}
	p.update(1, 1, 0, 0)
	p.close()
	if p.hasOutput() {
		t.Fatal("quiet emitted progress")
	}
}
