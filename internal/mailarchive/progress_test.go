package mailarchive

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressIntegrationCountsDownloadsAndExisting(t *testing.T) {
	c, done := testPeer(t, 42, false)
	var out bytes.Buffer
	p := newSyncProgress(&out, false)
	dir := t.TempDir()
	for round, want := range []string{"downloaded=1 existing=0", "downloaded=0 existing=1"} {
		p.beginFolder("INBOX", 1, 1)
		if _, _, err := syncFolder(c, dir, "INBOX", time.Time{}, imapSyncOptions{progress: p}); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		text := out.String()
		received := p.received
		p.mu.Unlock()
		if (round == 0 && received == 0) || (round == 1 && received != 0) {
			t.Fatalf("round %d: received=%d", round, received)
		}
		if !strings.Contains(text, want) || !strings.Contains(text, "100% 1/1") {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	p.close()
	c.Close()
	<-done
	if strings.Contains(out.String(), "\033") {
		t.Fatal("redirected progress contains terminal escapes")
	}

}

type progressEvents struct {
	mu     sync.Mutex
	data   bytes.Buffer
	events chan string
}

func (w *progressEvents) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, e := w.data.Write(b)
	select {
	case w.events <- string(b):
	default:
	}
	return n, e
}

func TestProgressHeartbeatWhileBodyIsInFlight(t *testing.T) {
	w := &progressEvents{events: make(chan string, 20)}
	p := &syncProgress{out: w, columns: 80, started: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	p.beginFolder("INBOX", 1, 1)
	p.listed(2)
	p.message("7")
	go p.run(10 * time.Millisecond)
	writer := progressWriter{io.Discard, p}
	if _, err := writer.Write(make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	defer p.close()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case text := <-w.events:
			if strings.Contains(text, "UID=7") && strings.Contains(text, "0/2") && strings.Contains(text, "received=2.0 KiB") {
				return
			}
		case <-timer.C:
			t.Fatal("no progress while a message body is still in flight")
		}
	}
}

func TestTerminalProgressAndPause(t *testing.T) {
	var out bytes.Buffer
	p := &syncProgress{out: &out, terminal: true, columns: 80, started: time.Now()}
	p.beginFolder("INBOX", 1, 2)
	p.listed(1)
	p.finish("ignored")
	p.pause()
	text := out.String()
	if !strings.Contains(text, "\033[1A") || !strings.Contains(text, "ignored=1") || !strings.Contains(text, "100% 1/1") {
		t.Fatal("missing terminal progress", text)
	}
	n := out.Len()
	p.mu.Lock()
	p.renderLocked()
	p.mu.Unlock()
	if out.Len() != n {
		t.Fatal("paused progress overwrote summary")
	}
}

func TestQuietProgress(t *testing.T) {
	var out bytes.Buffer
	p := newSyncProgress(&out, true)
	if p != nil {
		t.Fatal("quiet created progress reporter")
	}
	p.beginFolder("INBOX", 1, 1)
	p.listed(1)
	p.message("7")
	p.finish("downloaded")
	p.pause()
	p.close()
	if out.Len() != 0 {
		t.Fatal("quiet emitted progress")
	}
}

func TestPOP3ReportsProgress(t *testing.T) {
	conn, done := popPeer(t, false)
	var log, out bytes.Buffer
	p := newSyncProgress(&log, false)
	err := syncPOP3(conn, t.TempDir(), "test:995", "user@hust.edu.cn", "dedicated-password", &out, p)
	p.close()
	conn.Close()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if p.received == 0 || !strings.Contains(log.String(), "100% 1/1") || !strings.Contains(log.String(), "downloaded=1") {
		t.Fatalf("POP3 progress: %s", &log)
	}
	if strings.Contains(out.String(), "█") || !strings.Contains(out.String(), "Complete:") {
		t.Fatal("progress polluted stdout summary")
	}
}
