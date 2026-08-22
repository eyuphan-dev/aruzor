package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"aruzor/internal/auth"
)

// First-run setup. Before this existed, the only way to get an account on a
// fresh install was ARUZOR_ADMIN_PASSWORD — and with it unset the server
// generated a random password and printed it to the log, which meant the
// first thing a new user had to do was go read container logs. Worse, the
// documented default was a weak password that installs kept forever.
//
// The endpoints below are unauthenticated by necessity: there is nobody to
// authenticate as yet. They are safe only because they stop existing the
// moment an account does — every one of them re-checks that the users table
// is empty, and the insert itself is conditional on emptiness so two
// simultaneous requests cannot both succeed.

var errSetupComplete = errors.New("kurulum zaten tamamlanmis")

type setupStatusResponse struct {
	NeedsSetup bool `json:"needsSetup"`
}

type setupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSetupStatus tells the UI whether to show the setup screen or the
// login form. Deliberately reveals nothing beyond that single bit.
func (r *Router) handleSetupStatus(w http.ResponseWriter, req *http.Request) {
	count, err := r.db.CountUsers(req.Context())
	if err != nil {
		r.log.Error("kullanici sayisi okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{NeedsSetup: count == 0})
}

// handleSetup creates the first super_admin. Rate-limited with the login
// limiter: an unauthenticated POST that hashes a password is an easy way to
// burn CPU, and the two share the same "attempts from this address" budget.
func (r *Router) handleSetup(w http.ResponseWriter, req *http.Request) {
	if !r.loginLimiter.Allow(clientIP(req)) {
		writeError(w, http.StatusTooManyRequests, errTooManyAttempts)
		return
	}

	var body setupRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	if body.Email == "" || len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, errWeakCredentials)
		return
	}

	// Checked before hashing purely to fail fast; the insert below is what
	// actually guarantees only one account can be created this way.
	count, err := r.db.CountUsers(req.Context())
	if err != nil {
		r.log.Error("kullanici sayisi okunamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, errSetupComplete)
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		r.log.Error("sifre hashlenemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	created, err := r.db.CreateFirstUser(req.Context(), uuid.NewString(), body.Email, hash)
	if err != nil {
		r.log.Error("ilk kullanici olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if !created {
		// Another request won the race between the check above and here.
		writeError(w, http.StatusConflict, errSetupComplete)
		return
	}

	r.log.Info("kurulum tamamlandi, ilk super_admin olusturuldu", "email", body.Email, "remote", clientIP(req))
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}
