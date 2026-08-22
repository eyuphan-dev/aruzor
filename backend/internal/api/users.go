package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"aruzor/internal/auth"
)

var validRoles = map[string]bool{"viewer": true, "editor": true, "admin": true, "super_admin": true}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (r *Router) handleListUsers(w http.ResponseWriter, req *http.Request) {
	users, err := r.db.ListUsers(req.Context())
	if err != nil {
		r.log.Error("kullanici listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (r *Router) handleCreateUser(w http.ResponseWriter, req *http.Request) {
	var body createUserRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if body.Email == "" || len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, errWeakCredentials)
		return
	}
	if !validRoles[body.Role] {
		writeError(w, http.StatusBadRequest, errInvalidRole)
		return
	}

	existing, err := r.db.GetUserByEmail(req.Context(), body.Email)
	if err != nil {
		r.log.Error("kullanici sorgusu basarisiz", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, errUserExists)
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		r.log.Error("sifre hashlenemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	id := uuid.NewString()
	if err := r.db.CreateUser(req.Context(), id, body.Email, hash, body.Role); err != nil {
		r.log.Error("kullanici olusturulamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	claims := claimsFromContext(req.Context())
	r.log.Info("yeni kullanici olusturuldu", "olusturan", claims.Email, "yeni_kullanici", body.Email, "rol", body.Role)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "email": body.Email, "role": body.Role})
}

func (r *Router) handleDeleteUser(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	claims := claimsFromContext(req.Context())
	if claims != nil && claims.UserID == id {
		writeError(w, http.StatusBadRequest, errSelfDelete)
		return
	}
	if err := r.db.DeleteUser(req.Context(), id); err != nil {
		r.log.Error("kullanici silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.log.Warn("kullanici silindi", "silen", claims.Email, "silinen_id", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
