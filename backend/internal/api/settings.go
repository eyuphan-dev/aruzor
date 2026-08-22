package api

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// knownSettings whitelists which keys can be written via the API — an
// arbitrary key/value store writable by clients would otherwise let a
// compromised super_admin session stash unrelated data here.
var knownSettings = map[string]bool{
	"status_page_enabled": true,
	// A generic outgoing webhook: every alert and outage notice is POSTed
	// here as JSON, in addition to Telegram. Empty means disabled.
	"webhook_url": true,
}

// settingDefaults fills in a value for any known setting that has never
// been explicitly set, so the frontend doesn't need to guess a default.
var settingDefaults = map[string]string{
	"status_page_enabled": "false",
	"webhook_url":         "",
}

// validateSettingValue enforces the shape each known key needs. Most
// settings here are plain booleans; webhook_url is the one exception, so it
// gets its own case rather than a per-key validator registry that would be
// overkill for two kinds of value.
func validateSettingValue(key, value string) bool {
	if key == "webhook_url" {
		if value == "" {
			return true
		}
		u, err := url.Parse(value)
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	}
	return value == "true" || value == "false"
}

func (r *Router) handleListSettings(w http.ResponseWriter, req *http.Request) {
	stored, err := r.db.ListSettings(req.Context())
	if err != nil {
		r.log.Error("ayarlar alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	out := map[string]string{}
	for key, def := range settingDefaults {
		if v, ok := stored[key]; ok {
			out[key] = v
		} else {
			out[key] = def
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type updateSettingRequest struct {
	Value string `json:"value"`
}

func (r *Router) handleUpdateSetting(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if !knownSettings[key] {
		writeError(w, http.StatusBadRequest, errUnknownSetting)
		return
	}

	var body updateSettingRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || !validateSettingValue(key, body.Value) {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	if err := r.db.SetSetting(req.Context(), key, body.Value); err != nil {
		r.log.Error("ayar kaydedilemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	claims := claimsFromContext(req.Context())
	r.log.Info("ayar degistirildi", "kim", claims.Email, "ayar", key, "deger", body.Value)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
