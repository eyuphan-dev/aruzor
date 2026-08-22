package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/store"
)

var validOperators = map[string]bool{">": true, ">=": true, "<": true, "<=": true, "==": true}

type createAlertRequest struct {
	Name      string  `json:"name"`
	PromQL    string  `json:"promql"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
}

func (r *Router) handleListAlerts(w http.ResponseWriter, req *http.Request) {
	rules, err := r.db.ListAlertRules(req.Context())
	if err != nil {
		r.log.Error("alarm listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (r *Router) handleCreateAlert(w http.ResponseWriter, req *http.Request) {
	var body createAlertRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if body.Name == "" || body.PromQL == "" {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if !validOperators[body.Operator] {
		writeError(w, http.StatusBadRequest, errInvalidOperator)
		return
	}

	rule := store.AlertRule{
		ID:        uuid.NewString(),
		Name:      body.Name,
		PromQL:    body.PromQL,
		Operator:  body.Operator,
		Threshold: body.Threshold,
		Channel:   "telegram",
		Enabled:   true,
		LastState: "ok",
		CreatedAt: time.Now(),
	}
	if err := r.db.CreateAlertRule(req.Context(), rule); err != nil {
		r.log.Error("alarm olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

type updateAlertRequest struct {
	Enabled *bool `json:"enabled"`
}

func (r *Router) handleUpdateAlert(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body updateAlertRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if err := r.db.SetAlertRuleEnabled(req.Context(), id, *body.Enabled); err != nil {
		r.log.Error("alarm guncellenemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleDeleteAlert(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.db.DeleteAlertRule(req.Context(), id); err != nil {
		r.log.Error("alarm silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

const maxSnoozeMinutes = 30 * 24 * 60 // 30 days — generous ceiling against a fat-fingered value silencing a rule "forever"

type snoozeAlertRequest struct {
	Minutes int `json:"minutes"`
}

// handleSnoozeAlert suppresses notifications for a rule until now+minutes.
// minutes <= 0 clears an active snooze immediately.
func (r *Router) handleSnoozeAlert(w http.ResponseWriter, req *http.Request) {
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

	if err := r.db.SetAlertRuleSnooze(req.Context(), id, until); err != nil {
		r.log.Error("alarm susturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleAckAlert(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	now := time.Now()
	if err := r.db.SetAlertRuleAck(req.Context(), id, &now); err != nil {
		r.log.Error("alarm onaylanamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleAlertHistory(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	events, err := r.db.ListAlertEvents(req.Context(), id, 50)
	if err != nil {
		r.log.Error("alarm gecmisi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
