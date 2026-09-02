package traffic

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// backfillBytes is how much of a log file is read the very first time a
// source is seen.
//
// Starting at the end instead would be simpler, but it means the Traffic
// page shows an empty chart for the first hour after setup — the moment
// someone is most likely to conclude the feature does not work. Reading a
// slice of recent history gives the page something true to show
// immediately. It is only ever done once per file; after that the stored
// offset takes over.
const backfillBytes = 4 << 20 // 4 MiB

// maxLineBytes caps a single log line. Lines are attacker-influenced (a
// request URI and User-Agent both land in them), so the scanner must not be
// able to be pushed into allocating without limit.
const maxLineBytes = 64 << 10

// tailer follows one access-log file across restarts and rotations.
type tailer struct {
	id     string
	name   string
	path   string
	offset int64

	lines    int64
	unparsed int64

	// The last read error reported for this file, so a permanent condition
	// (a log Aruzor has no permission to open, a path that no longer exists)
	// is logged when it starts and when it clears rather than once a minute
	// forever. A glob over a log directory routinely matches one or two such
	// files, and drowning the server log in them is how real warnings get
	// missed.
	lastErr string
}

// read consumes everything written since the last call and hands each
// parsed entry to fn. It returns the number of lines it could not parse,
// which the API surfaces so a wrong log_format shows up as a number on the
// page instead of as a silently empty chart.
func (t *tailer) read(fn func(Entry)) error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()

	switch {
	case t.offset == 0 && t.lines == 0:
		// First sight of this file: take a recent slice rather than the
		// whole thing, which on a long-lived server can be gigabytes.
		if size > backfillBytes {
			t.offset = size - backfillBytes
		}
	case size < t.offset:
		// The file shrank, so it was rotated or truncated underneath us and
		// the old offset now points into unrelated content. Everything
		// written since rotation is still unread, so start from the top —
		// re-reading a little is recoverable, silently skipping a whole
		// day's traffic after a nightly logrotate is not.
		t.offset = 0
	case size == t.offset:
		return nil
	}

	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}

	reader := bufio.NewReaderSize(f, 64<<10)
	var consumed int64

	// A read that lands mid-write would end on a partial line. Only whole,
	// newline-terminated lines are consumed, and the offset advances by
	// exactly what was consumed, so the tail of a partial line is picked up
	// on the next pass instead of being parsed as a broken record.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		consumed += int64(len(line))
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		if len(line) > maxLineBytes {
			t.unparsed++
			continue
		}
		entry, perr := ParseLine(line)
		if perr != nil {
			t.unparsed++
			continue
		}
		t.lines++
		fn(entry)
	}

	t.offset += consumed
	return nil
}
