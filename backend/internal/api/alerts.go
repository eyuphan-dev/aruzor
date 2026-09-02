package api

import (
	"context"
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

func validateAlertFields(name, promql, operator string) error {
	if name == "" || promql == "" {
		return errInvalidBody
	}
	if !validOperators[operator] {
		return errInvalidOperator
	}
	return nil
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
	if err := validateAlertFields(body.Name, body.PromQL, body.Operator); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	rule := store.AlertRule{
		ID:        uuid.NewString(),
		Name:      body.Name,
		PromQL:    body.PromQL,
		Operator:  body.Operator,
		Threshold: body.Threshold,
		Enabled:   true,
		LastState: "ok",
		CreatedAt: time.Now(),
	}
	if err := r.db.CreateAlertRule(req.Context(), rule); err != nil {
		r.log.Error("alarm olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.auditFromClaims(req, "alert_created", rule.Name)
	writeJSON(w, http.StatusCreated, rule)
}

// updateAlertRequest covers two independent things through one endpoint:
// flipping Enabled (what the toggle switch on the list does) and editing the
// rule's definition (what an actual edit form does). Either can be sent
// alone or together — a bare {"enabled": false} keeps working exactly as it
// did before the rule became editable.
type updateAlertRequest struct {
	Enabled   *bool    `json:"enabled,omitempty"`
	Name      *string  `json:"name,omitempty"`
	PromQL    *string  `json:"promql,omitempty"`
	Operator  *string  `json:"operator,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
}

func (r *Router) handleUpdateAlert(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body updateAlertRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	editsDefinition := body.Name != nil || body.PromQL != nil || body.Operator != nil || body.Threshold != nil
	if body.Enabled == nil && !editsDefinition {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	if body.Enabled != nil {
		if err := r.db.SetAlertRuleEnabled(req.Context(), id, *body.Enabled); err != nil {
			r.log.Error("alarm guncellenemedi", "hata", err.Error())
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
	}

	if editsDefinition {
		// A partial edit (only the threshold changed, say) is filled in
		// from the rule's current values rather than requiring the client
		// to resend fields it isn't touching.
		current, err := r.findAlertRule(req.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, errAlertNotFound)
			return
		}
		name, promql, operator, threshold := current.Name, current.PromQL, current.Operator, current.Threshold
		if body.Name != nil {
			name = *body.Name
		}
		if body.PromQL != nil {
			promql = *body.PromQL
		}
		if body.Operator != nil {
			operator = *body.Operator
		}
		if body.Threshold != nil {
			threshold = *body.Threshold
		}
		if err := validateAlertFields(name, promql, operator); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := r.db.UpdateAlertRule(req.Context(), id, name, promql, operator, threshold); err != nil {
			r.log.Error("alarm duzenlenemedi", "hata", err.Error())
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
		r.auditFromClaims(req, "alert_updated", name)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// findAlertRule is a small linear lookup rather than a new store method: the
// rule list is never large enough (dozens, not millions) for this to matter,
// and it saves adding a GetAlertRule(id) query used from exactly one place.
func (r *Router) findAlertRule(ctx context.Context, id string) (store.AlertRule, error) {
	rules, err := r.db.ListAlertRules(ctx)
	if err != nil {
		return store.AlertRule{}, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return store.AlertRule{}, errAlertNotFound
}

func (r *Router) handleDeleteAlert(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	name := id
	if rule, err := r.findAlertRule(req.Context(), id); err == nil {
		name = rule.Name
	}
	if err := r.db.DeleteAlertRule(req.Context(), id); err != nil {
		r.log.Error("alarm silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.auditFromClaims(req, "alert_deleted", name)
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
