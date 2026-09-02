package traffic

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func entryAt(at time.Time, ip, path string, status int, bytes int64) Entry {
	return Entry{At: at, IP: ip, Path: path, Method: "GET", Status: status, Bytes: bytes, UserAgent: "curl/8.4.0"}
}

func TestWindow_KovaSayaclari(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 30, 0, time.UTC)
	w := newWindow()
	w.add("web", entryAt(base, "1.1.1.1", "/a", 200, 100))
	w.add("web", entryAt(base, "1.1.1.1", "/a", 301, 10))
	w.add("web", entryAt(base, "2.2.2.2", "/b", 403, 5))
	w.add("web", entryAt(base, "2.2.2.2", "/b", 500, 0))
	w.add("web", entryAt(base, "2.2.2.2", "/b", 404, 0))

	f := w.flush()
	if len(f.Buckets) != 1 {
		t.Fatalf("kova sayisi = %d, ayni dakikadaki istekler tek kovada toplanmali", len(f.Buckets))
	}
	b := f.Buckets[0]
	if b.At.Second() != 0 {
		t.Errorf("kova baslangici = %v, dakikaya yuvarlanmali", b.At)
	}
	if b.Requests != 5 || b.Bytes != 115 {
		t.Errorf("istek=%d bayt=%d", b.Requests, b.Bytes)
	}
	if b.S2xx != 1 || b.S3xx != 1 || b.S4xx != 2 || b.S5xx != 1 {
		t.Errorf("sinif dagilimi yanlis: 2xx=%d 3xx=%d 4xx=%d 5xx=%d", b.S2xx, b.S3xx, b.S4xx, b.S5xx)
	}
	// A 404 is a broken link or a crawler; only 401/403 mean "told no".
	if b.Unauthorized != 1 {
		t.Errorf("yetkisiz = %d, yalnizca 401/403 sayilmali (404 degil)", b.Unauthorized)
	}
}

func TestWindow_BoyutlarGirdiZamaninaYazilir(t *testing.T) {
	// Backfilling an existing log file replays hours of history in one
	// flush. If dims were stamped with "now", every one of those old
	// requests would land in the current minute and "top IPs in the last
	// hour" would be wrong for as long as the backfill stayed in range.
	old := time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)

	w := newWindow()
	w.add("web", entryAt(old, "1.1.1.1", "/a", 200, 1))
	w.add("web", entryAt(recent, "1.1.1.1", "/a", 200, 1))

	f := w.flush()
	seen := map[int64]bool{}
	for _, d := range f.Dims {
		if d.Dim == DimIP {
			seen[d.At.Unix()] = true
		}
	}
	if !seen[old.Unix()] || !seen[recent.Unix()] {
		t.Errorf("boyutlar girdi zamanina degil, tek bir ana yazilmis: %v", seen)
	}
}

func TestWindow_AcikBoyutlaraUstSinir(t *testing.T) {
	// A path-fuzzing scanner is the case this cap exists for: without it,
	// one minute of scanning writes a database row per probed URL.
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	w := newWindow()
	for i := 0; i < openDimTopN*4; i++ {
		w.add("web", entryAt(base, fmt.Sprintf("10.0.0.%d", i), fmt.Sprintf("/rasgele-%d", i), 404, 1))
	}

	counts := map[string]int{}
	for _, d := range w.flush().Dims {
		counts[d.Dim]++
	}
	if counts[DimPath] != openDimTopN {
		t.Errorf("yol sayisi = %d, %d ile sinirlanmaliydi", counts[DimPath], openDimTopN)
	}
	if counts[DimIP] != openDimTopN {
		t.Errorf("ip sayisi = %d, %d ile sinirlanmaliydi", counts[DimIP], openDimTopN)
	}
}

func TestWindow_UstSinirEnCokIsteyeniKorur(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	w := newWindow()
	for i := 0; i < openDimTopN*2; i++ {
		w.add("web", entryAt(base, "10.0.0."+strconv.Itoa(i), "/x", 200, 1))
	}
	// One IP well clear of the rest must survive the cut — the whole point
	// of the panel is that the loudest caller is visible.
	for i := 0; i < 50; i++ {
		w.add("web", entryAt(base, "203.0.113.9", "/x", 200, 1))
	}

	found := false
	for _, d := range w.flush().Dims {
		if d.Dim == DimIP && d.Key == "203.0.113.9" {
			found = true
			if d.Requests != 50 {
				t.Errorf("istek = %d, beklenen 50", d.Requests)
			}
		}
	}
	if !found {
		t.Error("en cok istek atan ip ust sinir uygulanirken elenmis")
	}
}

func TestWindow_EksikAlanlarBoyutUretmez(t *testing.T) {
	// $host and $upstream_addr are absent from the stock combined format.
	// Writing a placeholder would make those panels look populated while
	// telling the reader nothing.
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	w := newWindow()
	w.add("web", entryAt(base, "1.1.1.1", "/a", 200, 1))

	for _, d := range w.flush().Dims {
		if d.Dim == DimHost || d.Dim == DimService {
			t.Errorf("%s boyutu uretilmis (%q), log formatinda yokken uretilmemeli", d.Dim, d.Key)
		}
	}
}

func TestWindow_SonIstekListesiSinirli(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	w := newWindow()
	for i := 0; i < requestsPerFlush*3; i++ {
		w.add("web", entryAt(base.Add(time.Duration(i)*time.Second), "1.1.1.1", "/"+strconv.Itoa(i), 200, 1))
	}
	f := w.flush()
	if len(f.Requests) != requestsPerFlush {
		t.Fatalf("son istek sayisi = %d, %d olmaliydi", len(f.Requests), requestsPerFlush)
	}
	// Dropping from the front is what keeps this a "recent" list.
	last := f.Requests[len(f.Requests)-1]
	if last.Path != "/"+strconv.Itoa(requestsPerFlush*3-1) {
		t.Errorf("en yeni istek korunmamis: %q", last.Path)
	}
}

func TestTailer_SurdurmeVeDondurme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	line := func(p string) string {
		return `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "GET ` + p + ` HTTP/1.1" 200 10 "-" "-"` + "\n"
	}
	if err := os.WriteFile(path, []byte(line("/a")+line("/b")), 0o600); err != nil {
		t.Fatal(err)
	}

	tl := &tailer{id: "x", name: "web", path: path}
	var got []string
	collect := func(e Entry) { got = append(got, e.Path) }

	if err := tl.read(collect); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ilk okumada %d satir, 2 bekleniyordu", len(got))
	}

	// A second pass with nothing appended must not re-count what was
	// already read, or every restart would inflate the totals.
	got = nil
	if err := tl.read(collect); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("degismemis dosya yeniden okundu: %v", got)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(line("/c"))
	f.Close()

	got = nil
	if err := tl.read(collect); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "/c" {
		t.Fatalf("eklenen satir okunamadi: %v", got)
	}

	// logrotate replaces the file with a shorter one. The stored offset now
	// points into unrelated content, so reading must restart from the top
	// rather than skipping everything written since the rotation.
	if err := os.WriteFile(path, []byte(line("/after-rotate")), 0o600); err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := tl.read(collect); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "/after-rotate" {
		t.Fatalf("dondurme sonrasi okuma basarisiz: %v", got)
	}
}

func TestTailer_YarimSatirBeklenir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	full := `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "GET /a HTTP/1.1" 200 10 "-" "-"` + "\n"
	partial := `1.2.3.4 - - [02/Sep/2026:08:44:12 +0000] "GET /b HTTP`

	if err := os.WriteFile(path, []byte(full+partial), 0o600); err != nil {
		t.Fatal(err)
	}
	tl := &tailer{id: "x", name: "web", path: path}
	var got []string
	if err := tl.read(func(e Entry) { got = append(got, e.Path) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("yarim yazilmis satir islenmis: %v", got)
	}

	// Once the writer finishes the line it must be read whole, not as the
	// leftover half of a broken record.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`/1.1" 200 10 "-" "-"` + "\n")
	f.Close()

	got = nil
	if err := tl.read(func(e Entry) { got = append(got, e.Path) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "/b" {
		t.Fatalf("tamamlanan satir okunamadi: %v", got)
	}
	if tl.unparsed != 0 {
		t.Errorf("cozumlenemeyen = %d, yarim satir hata olarak sayilmamali", tl.unparsed)
	}
}

func TestParsePaths(t *testing.T) {
	got := parsePaths(" web=/var/log/nginx/access.log , /www/wwwlogs/*.log ,, ")
	if len(got) != 2 {
		t.Fatalf("giris sayisi = %d", len(got))
	}
	if got[0].name != "web" || got[0].glob != "/var/log/nginx/access.log" {
		t.Errorf("isimli giris yanlis: %+v", got[0])
	}
	if got[1].name != "" || got[1].glob != "/www/wwwlogs/*.log" {
		t.Errorf("isimsiz giris yanlis: %+v", got[1])
	}
}

func TestLooksLikeAccessLog(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/www/wwwlogs/ornek.com.log", true},
		{"/var/log/nginx/access.log", true},
		{"/www/wwwlogs/nginx_error.log", false},
		{"/www/wwwlogs/ornek.com.error.log", false},
		{"/var/log/nginx/access.log.1", false},
		{"/var/log/nginx/access.log.2.gz", false},
	}
	for _, c := range cases {
		if got := looksLikeAccessLog(c.path); got != c.want {
			t.Errorf("looksLikeAccessLog(%q) = %v, beklenen %v", c.path, got, c.want)
		}
	}
}

func TestSourceName(t *testing.T) {
	if got := sourceName("", "/www/wwwlogs/panel.ornek.com.log"); got != "panel.ornek.com" {
		t.Errorf("site basina log dosyasi site adiyla adlandirilmali, alinan %q", got)
	}
	if got := sourceName("kenar", "/var/log/nginx/access.log"); got != "kenar" {
		t.Errorf("yapilandirilmis isim ezilmis: %q", got)
	}
}
