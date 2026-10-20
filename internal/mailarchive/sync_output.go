package mailarchive

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/width"
)

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

type syncRecord struct {
	Type       string `json:"type"`
	Folder     string `json:"folder,omitempty"`
	Downloaded int    `json:"downloaded"`
	Existing   int    `json:"existing"`
	Ignored    int    `json:"ignored"`
	Archive    string `json:"archive,omitempty"`
	Cutoff     string `json:"cutoff,omitempty"`
}

type syncOutput struct {
	out   io.Writer
	json  bool
	width int
}

func columnWidth(text string) int {
	n := 0
	for _, r := range text {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			continue
		}
		n++
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n++
		}
	}
	return n
}

func (s *syncOutput) header(boxes []string) error {
	if s.json {
		return nil
	}
	s.width = len("FOLDER")
	for _, box := range boxes {
		s.width = max(s.width, columnWidth(strconv.Quote(box)))
	}
	_, err := fmt.Fprintf(s.out, "%s%s  %10s  %10s  %10s\n", "FOLDER", strings.Repeat(" ", s.width-len("FOLDER")), "DOWNLOADED", "EXISTING", "IGNORED")
	return err
}

func (s *syncOutput) folder(box string, downloaded, existing, ignored int) error {
	if s.json {
		return writeJSON(s.out, syncRecord{Type: "folder", Folder: box, Downloaded: downloaded, Existing: existing, Ignored: ignored})
	}
	label := strconv.Quote(box)
	_, err := fmt.Fprintf(s.out, "%s%s  %10d  %10d  %10d\n", label, strings.Repeat(" ", max(0, s.width-columnWidth(label))), downloaded, existing, ignored)
	return err
}

func (s *syncOutput) complete(root string, downloaded, existing, ignored int, cutoff time.Time) error {
	if s.json {
		record := syncRecord{Type: "complete", Downloaded: downloaded, Existing: existing, Ignored: ignored, Archive: root}
		if !cutoff.IsZero() {
			record.Cutoff = cutoff.Format(time.RFC3339)
		}
		return writeJSON(s.out, record)
	}
	if ignored > 0 {
		_, err := fmt.Fprintf(s.out, "Complete: downloaded=%d ignored=%d archive=%s\n", downloaded, ignored, root)
		return err
	}
	_, err := fmt.Fprintf(s.out, "Complete: downloaded=%d archive=%s\n", downloaded, root)
	return err
}

func writeFolders(out io.Writer, boxes []string, asJSON bool) error {
	for _, box := range boxes {
		if asJSON {
			if err := writeJSON(out, map[string]string{"folder": box}); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(out, "%q\n", box); err != nil {
			return err
		}
	}
	return nil
}
