package api

import (
	"encoding/json"
	"net/http"
)

type saveDashboardRequest struct {
	Title      string          `json:"title"`
	Definition json.RawMessage `json:"definition"`
}

func (r *Router) handleGetDashboard(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	d, err := r.db.GetDashboard(req.Context(), id)
	if err != nil {
		r.log.Error("dashboard okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, errDashboardNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         d.ID,
		"title":      d.Title,
		"definition": json.RawMessage(d.Definition),
		"updatedAt":  d.UpdatedAt,
	})
}

func (r *Router) handleSaveDashboard(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body saveDashboardRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Title == "" || len(body.Definition) == 0 {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if err := r.db.UpsertDashboard(req.Context(), id, body.Title, string(body.Definition)); err != nil {
		r.log.Error("dashboard kaydedilemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleListDashboards(w http.ResponseWriter, req *http.Request) {
	dashboards, err := r.db.ListDashboards(req.Context())
	if err != nil {
		r.log.Error("dashboard listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, dashboards)
}

func (r *Router) handleDeleteDashboard(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")

	count, err := r.db.CountDashboards(req.Context())
	if err != nil {
		r.log.Error("dashboard sayisi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if count <= 1 {
		writeError(w, http.StatusBadRequest, errCannotDeleteLastDashboard)
		return
	}

	if err := r.db.DeleteDashboard(req.Context(), id); err != nil {
		r.log.Error("dashboard silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
