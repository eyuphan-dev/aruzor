package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/store"
)

var validMonitorTypes = map[string]bool{"http": true, "tcp": true}

const monitorUptimeWindow = 30 * 24 * time.Hour

type createMonitorRequest struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Target          string `json:"target"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

// monitorResponse adds the computed uptime% to the stored monitor fields —
// kept out of the DB so it's always derived fresh from check history.
type monitorResponse struct {
	store.Monitor
	UptimePercent *float64 `json:"uptimePercent,omitempty"`
}

func (r *Router) handleListMonitors(w http.ResponseWriter, req *http.Request) {
	monitors, err := r.db.ListMonitors(req.Context())
	if err != nil {
		r.log.Error("izleme listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	out := make([]monitorResponse, 0, len(monitors))
	for _, m := range monitors {
		resp := monitorResponse{Monitor: m}
		if pct, ok, err := r.db.UptimePercent(req.Context(), m.ID, time.Now().Add(-monitorUptimeWindow)); err == nil && ok {
			resp.UptimePercent = &pct
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

func (r *Router) handleCreateMonitor(w http.ResponseWriter, req *http.Request) {
	var body createMonitorRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" || body.Target == "" {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if !validMonitorTypes[body.Type] {
		writeError(w, http.StatusBadRequest, errInvalidMonitorType)
		return
	}
	if body.IntervalSeconds < 15 || body.IntervalSeconds > 3600 {
		body.IntervalSeconds = 60
	}

	m := store.Monitor{
		ID:              uuid.NewString(),
		Name:            body.Name,
		Type:            body.Type,
		Target:          body.Target,
		IntervalSeconds: body.IntervalSeconds,
		CreatedAt:       time.Now(),
	}
	if err := r.db.CreateMonitor(req.Context(), m); err != nil {
		r.log.Error("izleme olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (r *Router) handleDeleteMonitor(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.db.DeleteMonitor(req.Context(), id); err != nil {
		r.log.Error("izleme silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSnoozeMonitor suppresses down notifications for a planned
// maintenance window. The check keeps running and history keeps recording —
// only the alert is held back — so this is safe to use ahead of a known
// deploy or reboot instead of disabling the monitor and losing the record
// of what happened while it was "off". minutes <= 0 clears an active snooze
// immediately. Shares its request shape and ceiling with alert snoozing
// (alerts.go, same package) since both answer the same question.
func (r *Router) handleSnoozeMonitor(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body snoozeAlertRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if body.Minutes > maxSnoozeMinutes {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	var until *time.Time
	if body.Minutes > 0 {
		t := time.Now().Add(time.Duration(body.Minutes) * time.Minute)
		until = &t
	}

	if err := r.db.SetMonitorSnooze(req.Context(), id, until); err != nil {
		r.log.Error("izleme susturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// dailyHistoryDays is the width of the status-page history strip — the
// convention every public status page uses (Statuspage, Better Stack,
// UptimeRobot all show 90 days), long enough to answer "was this reliable
// last quarter" without needing to page through anything.
const dailyHistoryDays = 90

type statusPageMonitor struct {
	Name          string              `json:"name"`
	OK            *bool               `json:"ok,omitempty"`
	UptimePercent *float64            `json:"uptimePercent,omitempty"`
	Summary       store.UptimeSummary `json:"uptimeSummary"`
	History       []store.DailyUptime `json:"dailyHistory"`
}

// handleStatusPage is intentionally unauthenticated (it's meant to be
// shared with people who don't have an Aruzor account) but only responds
// when a super_admin has explicitly turned it on via Settings, and only
// ever exposes a monitor's name/up-down/uptime% — never its target
// URL/host, which would leak internal network layout to the public.
func (r *Router) handleStatusPage(w http.ResponseWriter, req *http.Request) {
	value, _, err := r.db.GetSetting(req.Context(), "status_page_enabled")
	if err != nil {
		r.log.Error("ayar okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if value != "true" {
		writeError(w, http.StatusNotFound, errStatusPageDisabled)
		return
	}

	monitors, err := r.db.ListMonitors(req.Context())
	if err != nil {
		r.log.Error("izleme listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	now := time.Now()
	out := make([]statusPageMonitor, 0, len(monitors))
	for _, m := range monitors {
		entry := statusPageMonitor{Name: m.Name, OK: m.LastOK}
		if pct, ok, err := r.db.UptimePercent(req.Context(), m.ID, now.Add(-monitorUptimeWindow)); err == nil && ok {
			entry.UptimePercent = &pct
		}
		if summary, err := r.db.GetUptimeSummary(req.Context(), m.ID, now); err == nil {
			entry.Summary = summary
		}
		if history, err := r.db.DailyUptimeHistory(req.Context(), m.ID, dailyHistoryDays, now); err == nil {
			entry.History = history
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// monitorCheckLimit is how much history the detail view pulls. Enough to
// draw a useful latency trend and to show the last failures with their
// reasons, without shipping a month of rows to a phone.
const monitorCheckLimit = 120

// The timeline covers a day in half-hour steps. A day is the span somebody
// actually asks about ("it was slow this afternoon"), and 48 buckets is
// both readable as a row of bars on a phone and small enough to send.
const (
	timelineWindow = 24 * time.Hour
	timelineBucket = 30 * time.Minute
)

// timelineBucketResponse is one step of the day. It carries counts rather
// than a verdict: whether amber means "slow" or "briefly down" is a
// presentation question, and the client can change its mind about it
// without the stored data being wrong.
type timelineBucketResponse struct {
	Start  time.Time `json:"start"`
	Total  int       `json:"total"`
	Failed int       `json:"failed"`
	// Retried counts checks that only passed on a second or third attempt.
	// Those are the stalls the chat deliberately stays quiet about, so if
	// they were not carried here they would be invisible everywhere.
	Retried  int `json:"retried"`
	WorstMs  int `json:"worstMs"`
	MedianMs int `json:"medianMs"`
}

type monitorDetailResponse struct {
	monitorResponse
	Checks   []store.MonitorCheck     `json:"checks"`
	Timeline []timelineBucketResponse `json:"timeline"`
	// Summary/History back the SLA view: percentages across the standard
	// windows plus the day-by-day strip a status page shows.
	Summary store.UptimeSummary `json:"uptimeSummary"`
	History []store.DailyUptime `json:"dailyHistory"`
}

// buildTimeline folds a day of checks into fixed steps. Buckets with no
// checks are still emitted, with Total 0: a gap in monitoring is itself
// worth seeing, and dropping those steps would silently compress the day
// so the bars no longer line up with the clock.
func buildTimeline(checks []store.MonitorCheck, now time.Time) []timelineBucketResponse {
	start := now.Add(-timelineWindow).Truncate(timelineBucket)
	count := int(timelineWindow / timelineBucket)

	buckets := make([]timelineBucketResponse, count)
	latencies := make([][]int, count)
	for i := range buckets {
		buckets[i].Start = start.Add(time.Duration(i) * timelineBucket)
	}

	for _, c := range checks {
		i := int(c.CheckedAt.Sub(start) / timelineBucket)
		if i < 0 || i >= count {
			continue
		}
		buckets[i].Total++
		if !c.OK {
			buckets[i].Failed++
		} else if c.Attempts > 1 {
			buckets[i].Retried++
		}
		if c.LatencyMs > buckets[i].WorstMs {
			buckets[i].WorstMs = c.LatencyMs
		}
		latencies[i] = append(latencies[i], c.LatencyMs)
	}

	for i := range buckets {
		if len(latencies[i]) == 0 {
			continue
		}
		sort.Ints(latencies[i])
		buckets[i].MedianMs = latencies[i][len(latencies[i])/2]
	}
	return buckets
}

// handleMonitorChecks backs the detail view. It returns the monitor plus
// its recent checks, each carrying what the network stack reported — the
// interpretation of those facts is the client's job, not this endpoint's.
func (r *Router) handleMonitorChecks(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")

	monitors, err := r.db.ListMonitors(req.Context())
	if err != nil {
		r.log.Error("izleme listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	var found *store.Monitor
	for i := range monitors {
		if monitors[i].ID == id {
			found = &monitors[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, errMonitorNotFound)
		return
	}

	checks, err := r.db.ListMonitorChecks(req.Context(), id, monitorCheckLimit)
	if err != nil {
		r.log.Error("izleme gecmisi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	now := time.Now()
	window, err := r.db.ListMonitorChecksSince(req.Context(), id, now.Add(-timelineWindow))
	if err != nil {
		r.log.Error("izleme zaman cizelgesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	resp := monitorDetailResponse{
		monitorResponse: monitorResponse{Monitor: *found},
		Checks:          checks,
		Timeline:        buildTimeline(window, now),
	}
	if pct, ok, err := r.db.UptimePercent(req.Context(), id, now.Add(-monitorUptimeWindow)); err == nil && ok {
		resp.UptimePercent = &pct
	}
	if summary, err := r.db.GetUptimeSummary(req.Context(), id, now); err == nil {
		resp.Summary = summary
	}
	if history, err := r.db.DailyUptimeHistory(req.Context(), id, dailyHistoryDays, now); err == nil {
		resp.History = history
	}
	writeJSON(w, http.StatusOK, resp)
}
