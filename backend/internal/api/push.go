package api

import (
	"encoding/json"
	"net/http"
)

// handlePushVAPIDKey hands out the public half of the server's Web Push
// keypair. It is not a secret — every subscribing browser receives it — so
// this is deliberately the one push endpoint with no auth requirement, the
// same way a TLS certificate's public key needs none.
func (r *Router) handlePushVAPIDKey(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": r.vapidPublicKey})
}

type pushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type pushSubscribeRequest struct {
	Endpoint string   `json:"endpoint"`
	Keys     pushKeys `json:"keys"`
}

func (r *Router) handlePushSubscribe(w http.ResponseWriter, req *http.Request) {
	var body pushSubscribeRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil ||
		body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	if err := r.db.UpsertPushSubscription(req.Context(), body.Endpoint, body.Keys.P256dh, body.Keys.Auth); err != nil {
		r.log.Error("push kaydi olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

type pushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

func (r *Router) handlePushUnsubscribe(w http.ResponseWriter, req *http.Request) {
	var body pushUnsubscribeRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Endpoint == "" {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	if err := r.db.DeletePushSubscription(req.Context(), body.Endpoint); err != nil {
		r.log.Error("push kaydi silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
