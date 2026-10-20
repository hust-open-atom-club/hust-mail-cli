package mailarchive

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// This client deliberately exposes only receiving commands, never DELE.
type popReader struct{ c *textproto.Conn }

func (p popReader) command(format string, args ...any) error {
	if err := p.c.PrintfLine(format, args...); err != nil {
		return err
	}
	return p.status()
}

func (p popReader) status() error {
	line, err := p.c.ReadLine()
	if err != nil {
		return err
	}
	if line != "+OK" && !strings.HasPrefix(line, "+OK ") {
		return errors.New("POP3 server rejected command")
	}
	return nil
}

func syncPOP3(conn net.Conn, dest, host, user, password string, out io.Writer, reporters ...*syncProgress) error {
	progress := firstProgress(reporters)
	progress.setPhase("authenticating")
	if strings.ContainsAny(user+password, "\r\n") {
		return errors.New("invalid POP3 credentials")
	}
	p := popReader{textproto.NewConn(conn)}
	if err := p.status(); err != nil {
		return err
	}
	if err := p.command("USER %s", user); err != nil {
		return fmt.Errorf("POP3 USER: %w", err)
	}
	if err := p.command("PASS %s", password); err != nil {
		return fmt.Errorf("POP3 authentication failed: %w", err)
	}
	root, err := accountDir(dest, "pop3:"+host, user)
	if err != nil {
		return err
	}
	unlock, err := lockArchive(root)
	if err != nil {
		return err
	}
	defer unlock()
	progress.beginFolder("POP3", 1, 1)
	progress.setPhase("listing mail")
	if err := p.command("UIDL"); err != nil {
		return fmt.Errorf("UIDL required for incremental sync: %w", err)
	}
	lines, err := p.c.ReadDotLines()
	if err != nil {
		return err
	}
	type message struct {
		number int
		uid    string
	}
	messages := make([]message, 0, len(lines))
	seenUID, seenNum := map[string]bool{}, map[int]bool{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return errors.New("invalid UIDL response")
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil || n <= 0 || seenUID[fields[1]] || seenNum[n] {
			return errors.New("invalid or duplicate UIDL entry")
		}
		seenUID[fields[1]], seenNum[n] = true, true
		messages = append(messages, message{n, fields[1]})
	}
	progress.listed(len(messages))
	downloaded, skipped := 0, 0
	for _, m := range messages {
		progress.message(strconv.Itoa(m.number))
		hash := sha256.Sum256([]byte(m.uid))
		path := filepath.Join(root, fmt.Sprintf("%x.eml", hash))
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return errors.New("archive entry is not a regular file")
			}
			if info.Size() > 0 {
				skipped++
				progress.finish("existing")
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		err = atomicWrite(path, func(w io.Writer) error {
			w = progressWriter{w, progress}
			if err := p.command("RETR %d", m.number); err != nil {
				return err
			}
			// DotReader removes dot stuffing and normalizes line endings to LF.
			// Restore CRLF while streaming, without buffering attachments in memory.
			r := p.c.DotReader()
			buf := make([]byte, 32*1024)
			total := 0
			for {
				n, readErr := r.Read(buf)
				if n > 0 {
					total += n
					if _, err := io.WriteString(w, strings.ReplaceAll(string(buf[:n]), "\n", "\r\n")); err != nil {
						return err
					}
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					return readErr
				}
			}
			if total == 0 {
				return errors.New("empty POP3 message")
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("retrieve message %d: %w", m.number, err)
		}
		downloaded++
		progress.finish("downloaded")
	}
	if err := p.command("QUIT"); err != nil {
		return err
	}
	progress.pause()
	fmt.Fprintf(out, "Complete: downloaded=%d existing=%d archive=%s\n", downloaded, skipped, root)
	return nil
}
