package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"aruzor/internal/store"
)

// Read-only dashboard sharing. The link is a bearer capability: whoever has
// the URL sees that dashboard, with no account and no way to change
// anything. That is the point — sending a colleague a screenshot of a graph
// is what people do otherwise, and a screenshot is stale the moment it is
// taken.
//
// Two properties make it safe to hand out:
//
//   - The token is the only thing that grants access, so revoking is a
//     delete. Re-sharing mints a new token rather than returning the old
//     one; the usual reason to re-share is that the previous link went
//     somewhere it should not have.
//   - A shared viewer can only run the queries that dashboard's own panels
//     contain. Without that check, an unauthenticated endpoint that takes a
//     PromQL string would let anyone read every metric on the system and use
//     the backend to reach any host the datasource can.

var (
	errShareNotFound       = errors.New("paylasim baglantisi bulunamadi")
	errQueryNotOnDashboard = errors.New("bu sorgu paylasilan panoya ait degil")
)

// shareTokenBytes is deliberately generous: this token stands in for
// authentication and is likely to end up in chat logs and browser history,
// where it will sit for a long time.
const shareTokenBytes = 32

func newShareToken() (string, error) {
	buf := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

type shareResponse struct {
	Token string `json:"token"`
}

func (r *Router) handleCreateShare(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")

	dashboard, err := r.db.GetDashboard(req.Context(), id)
	if err != nil {
		r.log.Error("dashboard okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if dashboard == nil {
		writeError(w, http.StatusNotFound, errShareNotFound)
		return
	}

	token, err := newShareToken()
	if err != nil {
		r.log.Error("paylasim anahtari uretilemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if err := r.db.CreateShare(req.Context(), token, id); err != nil {
		r.log.Error("paylasim baglantisi olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	r.log.Info("dashboard paylasim baglantisi olusturuldu", "dashboard", id, "kullanici", claimsFromContext(req.Context()).Email)
	writeJSON(w, http.StatusCreated, shareResponse{Token: token})
}

func (r *Router) handleGetShare(w http.ResponseWriter, req *http.Request) {
	token, ok, err := r.db.GetShareToken(req.Context(), req.PathValue("id"))
	if err != nil {
		r.log.Error("paylasim baglantisi okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, shareResponse{Token: ""})
		return
	}
	writeJSON(w, http.StatusOK, shareResponse{Token: token})
}

func (r *Router) handleDeleteShare(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.db.DeleteShare(req.Context(), id); err != nil {
		r.log.Error("paylasim baglantisi silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.log.Info("dashboard paylasim baglantisi iptal edildi", "dashboard", id, "kullanici", claimsFromContext(req.Context()).Email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type sharedDashboardResponse struct {
	Title      string          `json:"title"`
	Definition json.RawMessage `json:"definition"`
}

// resolveShared looks up the dashboard behind a token. Every unauthenticated
// endpoint below starts here, and all of them answer a bad token the same
// way — a 404 that does not distinguish "never existed" from "revoked".
func (r *Router) resolveShared(w http.ResponseWriter, req *http.Request) (*store.Dashboard, bool) {
	if !r.statusPageLimiter.Allow(clientIP(req)) {
		writeError(w, http.StatusTooManyRequests, errTooManyAttempts)
		return nil, false
	}

	id, ok, err := r.db.GetSharedDashboardID(req.Context(), req.PathValue("token"))
	if err != nil {
		r.log.Error("paylasim cozumlenemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return nil, false
	}
	if !ok {
		writeError(w, http.StatusNotFound, errShareNotFound)
		return nil, false
	}

	dashboard, err := r.db.GetDashboard(req.Context(), id)
	if err != nil {
		r.log.Error("dashboard okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return nil, false
	}
	if dashboard == nil {
		writeError(w, http.StatusNotFound, errShareNotFound)
		return nil, false
	}
	return dashboard, true
}

func (r *Router) handleSharedDashboard(w http.ResponseWriter, req *http.Request) {
	dashboard, ok := r.resolveShared(w, req)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, sharedDashboardResponse{
		Title:      dashboard.Title,
		Definition: json.RawMessage(dashboard.Definition),
	})
}

// handleSharedQueryRange runs one of the shared dashboard's own queries.
// The requested query must match a panel on that dashboard exactly; this is
// what keeps an unauthenticated PromQL endpoint from being a way to read the
// whole metrics store.
func (r *Router) handleSharedQueryRange(w http.ResponseWriter, req *http.Request) {
	dashboard, ok := r.resolveShared(w, req)
	if !ok {
		return
	}

	query := req.URL.Query().Get("query")
	if !dashboardHasQuery(dashboard.Definition, query) {
		r.log.Warn("paylasilan panoda olmayan sorgu reddedildi", "remote", clientIP(req))
		writeError(w, http.StatusForbidden, errQueryNotOnDashboard)
		return
	}

	start, end, step, err := rangeParams(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	data, err := r.prom.QueryRange(req.Context(), query, start, end, step)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeRaw(w, http.StatusOK, data)
}

// dashboardHasQuery reports whether the stored definition contains a panel
// with exactly this query. Comparison is exact on purpose: any loosening
// (prefix, substring, normalised whitespace) reopens the hole this check
// exists to close.
func dashboardHasQuery(definition string, query string) bool {
	if query == "" {
		return false
	}
	var def struct {
		Panels []struct {
			PromQL string `json:"promql"`
		} `json:"panels"`
	}
	if err := json.Unmarshal([]byte(definition), &def); err != nil {
		return false
	}
	for _, p := range def.Panels {
		if p.PromQL == query {
			return true
		}
	}
	return false
}

// rangeParams parses the start/end/step trio for the shared range endpoint.
func rangeParams(req *http.Request) (time.Time, time.Time, time.Duration, error) {
	q := req.URL.Query()
	start, err := parseUnixTime(q.Get("start"))
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	end, err := parseUnixTime(q.Get("end"))
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	step, err := time.ParseDuration(orDefault(q.Get("step"), "15s"))
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}
	return start, end, step, nil
}
