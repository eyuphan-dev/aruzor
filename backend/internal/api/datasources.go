package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/store"
)

const defaultDatasourceID = "default"

type createDatasourceRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

func (r *Router) handleListDatasources(w http.ResponseWriter, req *http.Request) {
	datasources, err := r.db.ListDatasources(req.Context())
	if err != nil {
		r.log.Error("veri kaynaklari alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, datasources)
}

func (r *Router) handleCreateDatasource(w http.ResponseWriter, req *http.Request) {
	var body createDatasourceRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if body.Type == "" {
		body.Type = "prometheus"
	}

	ds := store.Datasource{ID: uuid.NewString(), Name: body.Name, URL: body.URL, Type: body.Type, CreatedAt: time.Now()}
	if err := r.db.CreateDatasource(req.Context(), ds); err != nil {
		r.log.Error("veri kaynagi olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, ds)
}

func (r *Router) handleDeleteDatasource(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == defaultDatasourceID {
		writeError(w, http.StatusBadRequest, errCannotDeleteDefault)
		return
	}
	if err := r.db.DeleteDatasource(req.Context(), id); err != nil {
		r.log.Error("veri kaynagi silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
