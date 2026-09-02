package traffic

import (
	"bufio"
	"os"
	"testing"
)

// TestParseLine_GercekOrnek runs the parser over a real access log when one
// is pointed at by ARUZOR_TEST_ACCESS_LOG, and fails if too much of it goes
// unparsed. Unit tests prove the parser handles the shapes it was written
// for; only a real file proves it handles the shapes an actual server
// produces — scanner junk, IPv6 clients, oddly escaped agents.
//
// Skipped by default so `go test ./...` needs no fixture.
func TestParseLine_GercekOrnek(t *testing.T) {
	path := os.Getenv("ARUZOR_TEST_ACCESS_LOG")
	if path == "" {
		t.Skip("ARUZOR_TEST_ACCESS_LOG tanimli degil")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("ornek log acilamadi: %v", err)
	}
	defer f.Close()

	var total, failed int
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		total++
		if _, err := ParseLine(line); err != nil {
			failed++
			if failed <= 5 {
				t.Logf("cozumlenemedi: %s", line)
			}
		}
	}
	if total == 0 {
		t.Skip("ornek log bos")
	}
	ratio := float64(failed) / float64(total) * 100
	t.Logf("%d satirin %d tanesi cozumlenemedi (%%%.2f)", total, failed, ratio)
	if ratio > 1 {
		t.Errorf("cozumlenemeyen oran %%%.2f, %%1 esiginin uzerinde", ratio)
	}
}
