package alerts

import (
	"testing"
	"time"
)

func pendingAt(now time.Time, ago time.Duration) *time.Time {
	t := now.Add(-ago)
	return &t
}

// A single sample above the threshold must not fire immediately — this is
// the whole point of firingDebounce. Without it, a metric hovering near its
// threshold could fire and resolve on every evaluation tick.
func TestDecideRuleTransition_TekOrnekAteslemez(t *testing.T) {
	now := time.Now()
	d := decideRuleTransition(ruleInput{lastState: "ok", firing: true, now: now})

	if d.commit {
		t.Fatal("tek ornekte kural taahhut edilmemeliydi")
	}
	if d.pendingState != "firing" || d.pendingSince == nil {
		t.Errorf("bekleme durumu baslatilmadi: %+v", d)
	}
}

// Once a breach has lasted the full debounce window, it commits and asks to
// notify.
func TestDecideRuleTransition_PencereDolunca(t *testing.T) {
	now := time.Now()
	d := decideRuleTransition(ruleInput{
		lastState: "ok", pendingState: "firing", pendingSince: pendingAt(now, firingDebounce+time.Second),
		firing: true, now: now,
	})

	if !d.commit || d.newState != "firing" || !d.notify {
		t.Fatalf("pencere dolmus bir ihlal tetiklenmeliydi: %+v", d)
	}
}

// Still inside the window: keep counting, don't commit yet, and the
// original start time must not be reset by every subsequent tick.
func TestDecideRuleTransition_PencereIcindeSayimDevamEder(t *testing.T) {
	now := time.Now()
	since := pendingAt(now, firingDebounce/2)
	d := decideRuleTransition(ruleInput{
		lastState: "ok", pendingState: "firing", pendingSince: since,
		firing: true, now: now,
	})

	if d.commit {
		t.Fatal("pencere dolmadan taahhut edilmemeliydi")
	}
	if d.pendingSince != since {
		t.Error("bekleme baslangici sifirlandi, sayim yeniden basladi")
	}
}

// The value dipping back below the threshold before the window elapses must
// clear the pending breach — the whole reason a spike-then-recover never
// fires is that nothing remembers the spike once it's over.
func TestDecideRuleTransition_ErkenDuzelmeBeklemeyiTemizler(t *testing.T) {
	now := time.Now()
	d := decideRuleTransition(ruleInput{
		lastState: "ok", pendingState: "firing", pendingSince: pendingAt(now, time.Minute),
		firing: false, now: now,
	})

	if d.commit || d.notify {
		t.Fatalf("henuz atesleme taahhut edilmemisken bildirim gitmemeliydi: %+v", d)
	}
	if d.pendingState != "" || d.pendingSince != nil {
		t.Errorf("eski bekleme kaydi temizlenmedi: %+v", d)
	}
}

// Recovery from an actually-firing rule is instant — no debounce — the same
// asymmetry the uptime checker applies (outages need a sustained window,
// recoveries are reported the moment they're true).
func TestDecideRuleTransition_DuzelmeAninda(t *testing.T) {
	now := time.Now()
	d := decideRuleTransition(ruleInput{lastState: "firing", firing: false, now: now})

	if !d.commit || d.newState != "ok" || !d.notify {
		t.Fatalf("duzelme aninda bildirilmeliydi: %+v", d)
	}
}

// Steady state (already at the state the condition implies) is a no-op —
// nothing to persist, nothing to send.
func TestDecideRuleTransition_DegismeyenDurumHicbirSeyYapmaz(t *testing.T) {
	now := time.Now()
	for _, in := range []ruleInput{
		{lastState: "ok", firing: false, now: now},
		{lastState: "firing", firing: true, now: now},
	} {
		d := decideRuleTransition(in)
		if d.commit || d.notify || d.pendingState != "" {
			t.Errorf("degismeyen durum bir seyler uretti: girdi=%+v cikti=%+v", in, d)
		}
	}
}

// A rule that was already firing and pending recovery (pendingState "ok"
// counted for a hypothetical symmetric debounce) must not have that stale
// pending state leak into a fresh firing breach — a flap in one direction
// must not borrow the clock started in the other.
func TestDecideRuleTransition_TersYonBeklemesiKarismaz(t *testing.T) {
	now := time.Now()
	d := decideRuleTransition(ruleInput{
		lastState: "ok", pendingState: "ok", pendingSince: pendingAt(now, time.Hour),
		firing: true, now: now,
	})
	if d.commit {
		t.Fatal("yanlis yondeki eski bekleme kaydiyla hemen taahhut edildi")
	}
	if d.pendingSince == nil || !d.pendingSince.Equal(now) {
		t.Errorf("yeni yon icin sayim eski zamandan degil simdiden baslamali: %+v", d)
	}
}
