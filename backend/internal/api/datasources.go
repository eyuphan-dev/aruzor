package api

import (
	"encoding/json"
	"net/http"
	"net/url"
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

// validateDatasourceURL rejects a URL that can't possibly be a Prometheus
// endpoint before it is ever handed to the server-side HTTP client this
// creates — the same "reject rather than silently misbehave" standard
// applied to every other admin-only input that reaches an outbound request.
// It does not attempt to block internal addresses: an operator's own
// Prometheus routinely lives on a private IP, and this trust boundary is
// already admin-only for exactly that reason (see the router's routing
// comment on datasources/monitors).
func validateDatasourceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errInvalidDatasourceURL
	}
	return nil
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
	if err := validateDatasourceURL(body.URL); err != nil {
		writeError(w, http.StatusBadRequest, err)
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
	// The URL is the whole point of recording this: a datasource is a
	// server-side HTTP client the admin role can point anywhere, and this
	// is the only record of who pointed it where.
	r.auditFromClaims(req, "datasource_created", ds.Name+" -> "+ds.URL)
	writeJSON(w, http.StatusCreated, ds)
}

func (r *Router) handleUpdateDatasource(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body createDatasourceRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if err := validateDatasourceURL(body.URL); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := r.db.UpdateDatasource(req.Context(), id, body.Name, body.URL); err != nil {
		r.log.Error("veri kaynagi duzenlenemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.auditFromClaims(req, "datasource_updated", body.Name+" -> "+body.URL)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleDeleteDatasource(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == defaultDatasourceID {
		writeError(w, http.StatusBadRequest, errCannotDeleteDefault)
		return
	}
	name := id
	if ds, err := r.db.GetDatasource(req.Context(), id); err == nil && ds != nil {
		name = ds.Name
	}
	if err := r.db.DeleteDatasource(req.Context(), id); err != nil {
		r.log.Error("veri kaynagi silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.auditFromClaims(req, "datasource_deleted", name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
