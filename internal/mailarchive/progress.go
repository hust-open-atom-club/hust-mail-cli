package mailarchive

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
	"golang.org/x/text/width"
)

// Progress belongs on stderr: stdout keeps the completed-folder summaries.
// One goroutine redraws even while a network operation is waiting for data.
type syncProgress struct {
	mu                                                                              sync.Mutex
	out                                                                             io.Writer
	terminal                                                                        bool
	columns                                                                         int
	stop, done                                                                      chan struct{}
	started, last                                                                   time.Time
	folder, phase, uid                                                              string
	folderIndex, folders, total, processed, downloaded, existing, filtered, ignored int
	received                                                                        int64
	known, paused, visible                                                          bool
}

func newSyncProgress(out io.Writer, quiet bool) *syncProgress {
	if quiet {
		return nil
	}
	p := &syncProgress{out: out, columns: 80, stop: make(chan struct{}), done: make(chan struct{}), started: time.Now(), phase: "connecting"}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		p.terminal = true
		if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 1 {
			p.columns = cols
		}
	}
	p.mu.Lock()
	p.renderLocked()
	p.mu.Unlock()
	interval := 5 * time.Second
	if p.terminal {
		interval = 250 * time.Millisecond
	}
	go p.run(interval)
	return p
}

func (p *syncProgress) run(interval time.Duration) {
	defer close(p.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			p.renderLocked()
			p.mu.Unlock()
		case <-p.stop:
			return
		}
	}
}

func (p *syncProgress) close() {
	if p == nil {
		return
	}
	close(p.stop)
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.visible {
		fmt.Fprintln(p.out)
		p.visible = false
	}
}

// Pause before stdout summaries or warnings, so redraws never overwrite them.
func (p *syncProgress) pause() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = true
	if p.visible {
		fmt.Fprint(p.out, "\r\033[2K\033[1A\r\033[2K")
		p.visible = false
	}
}

func (p *syncProgress) setPhase(phase string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = phase
	p.renderLocked()
}

func (p *syncProgress) beginFolder(name string, index, total int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.folder = terminalText(name)
	p.folderIndex = index
	p.folders = total
	p.phase = "selecting"
	p.uid = ""
	p.total = 0
	p.processed = 0
	p.downloaded = 0
	p.existing = 0
	p.filtered = 0
	p.ignored = 0
	p.received = 0
	p.known = false
	p.paused = false
	p.renderLocked()
}

func (p *syncProgress) listed(total int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
	p.known = true
	p.phase = "syncing"
	p.renderLocked()
}

func (p *syncProgress) message(uid string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.uid = uid
	p.phase = "checking/downloading"
}

func (p *syncProgress) addBytes(n int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.received += n
}

func (p *syncProgress) finish(kind string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processed++
	p.uid = ""
	p.phase = "syncing"
	switch kind {
	case "downloaded":
		p.downloaded++
	case "existing":
		p.existing++
	case "filtered":
		p.filtered++
	case "ignored":
		p.ignored++
	}
	if p.processed == p.total || time.Since(p.last) >= 5*time.Second {
		p.renderLocked()
	}
}

func (p *syncProgress) lines() (string, string) {
	title := "Sync: " + p.phase
	if p.folder != "" {
		title = fmt.Sprintf("Folder %d/%d %q: %s", p.folderIndex, p.folders, p.folder, p.phase)
	}
	if p.known {
		percent := 100
		if p.total > 0 {
			percent = int(int64(p.processed) * 100 / int64(p.total))
		}
		filled := percent * 20 / 100
		title = fmt.Sprintf("Folder %d/%d %q [%s%s] %3d%% %d/%d", p.folderIndex, p.folders, p.folder, strings.Repeat("█", filled), strings.Repeat("░", 20-filled), percent, p.processed, p.total)
		if p.uid != "" {
			title += " UID=" + p.uid
		}
	}
	stats := fmt.Sprintf("downloaded=%d existing=%d ignored=%d filtered=%d received=%s elapsed=%s", p.downloaded, p.existing, p.ignored, p.filtered, progressBytes(p.received), time.Since(p.started).Truncate(time.Second))
	if p.terminal {
		stats = fmt.Sprintf("new=%d existing=%d ignored=%d filtered=%d rx=%s time=%s", p.downloaded, p.existing, p.ignored, p.filtered, progressBytes(p.received), time.Since(p.started).Truncate(time.Second))
	}
	return title, stats
}

func (p *syncProgress) renderLocked() {
	if p.paused {
		return
	}
	title, stats := p.lines()
	if p.terminal {
		if p.visible {
			fmt.Fprint(p.out, "\r\033[1A")
		}
		fmt.Fprintf(p.out, "\r\033[2K%s\n\r\033[2K%s", progressClip(title, p.columns-1), progressClip(stats, p.columns-1))
		p.visible = true
	} else {
		fmt.Fprintf(p.out, "%s | %s\n", title, stats)
	}
	p.last = time.Now()
}

func progressBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return ""
}

// Keep wide mailbox names from wrapping and breaking the two-line display.
func progressClip(text string, columns int) string {
	var b strings.Builder
	used := 0
	for _, r := range text {
		w := 1
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			w = 2
		}
		if used+w > columns {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

type progressWriter struct {
	io.Writer
	progress *syncProgress
}

func (w progressWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	w.progress.addBytes(int64(n))
	return n, err
}

func firstProgress(progress []*syncProgress) *syncProgress {
	if len(progress) > 0 {
		return progress[0]
	}
	return nil
}

// IMAP sync settings keep progress optional for protocol tests.
type imapSyncOptions struct{ progress *syncProgress }

func firstIMAPOptions(options []imapSyncOptions) imapSyncOptions {
	if len(options) > 0 {
		return options[0]
	}
	return imapSyncOptions{}
}
