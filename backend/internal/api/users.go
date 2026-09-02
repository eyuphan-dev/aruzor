package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"aruzor/internal/auth"
	"aruzor/internal/store"
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
	r.auditFromClaims(req, "user_created", body.Email+" ("+body.Role+")")
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "email": body.Email, "role": body.Role})
}

func (r *Router) findUser(ctx context.Context, id string) (store.User, error) {
	users, err := r.db.ListUsers(ctx)
	if err != nil {
		return store.User{}, err
	}
	for _, u := range users {
		if u.ID == id {
			return u, nil
		}
	}
	return store.User{}, errUserNotFound
}

// updateUserRequest covers the two things there is no other way to do to an
// existing account: change its role, and reset its password. There is no
// self-service "forgot password" — this app never sends email — so a
// super_admin doing this here is the only recovery path; before it existed,
// fixing a typo'd role or a forgotten password meant deleting the account
// and creating a new one, losing its id.
type updateUserRequest struct {
	Role     *string `json:"role,omitempty"`
	Password *string `json:"password,omitempty"`
}

func (r *Router) handleUpdateUser(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body updateUserRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}
	if body.Role == nil && body.Password == nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	target, err := r.findUser(req.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, errUserNotFound)
		return
	}

	if body.Role != nil {
		if !validRoles[*body.Role] {
			writeError(w, http.StatusBadRequest, errInvalidRole)
			return
		}
		if target.Role == "super_admin" && *body.Role != "super_admin" {
			if err := r.rejectIfLastSuperAdmin(w, req); err != nil {
				return
			}
		}
		if err := r.db.UpdateUserRole(req.Context(), id, *body.Role); err != nil {
			r.log.Error("kullanici rolu guncellenemedi", "hata", err.Error())
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
		r.auditFromClaims(req, "user_role_changed", target.Email+" -> "+*body.Role)
	}

	if body.Password != nil {
		if len(*body.Password) < 8 {
			writeError(w, http.StatusBadRequest, errWeakCredentials)
			return
		}
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			r.log.Error("sifre hashlenemedi", "hata", err.Error())
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
		if err := r.db.UpdateUserPassword(req.Context(), id, hash); err != nil {
			r.log.Error("kullanici sifresi guncellenemedi", "hata", err.Error())
			writeError(w, http.StatusInternalServerError, errInternal)
			return
		}
		// The new password itself never touches a log line, only the fact
		// that it changed and who changed it.
		r.auditFromClaims(req, "user_password_reset", target.Email)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// rejectIfLastSuperAdmin writes a 400 and reports true (as an error, to
// short-circuit the caller with `if err := ...; err != nil { return }`)
// when the instance currently has exactly one super_admin — demoting or
// deleting that account would leave nobody able to manage users, settings
// or the audit log, with no registration flow to recover through.
func (r *Router) rejectIfLastSuperAdmin(w http.ResponseWriter, req *http.Request) error {
	count, err := r.db.CountSuperAdmins(req.Context())
	if err != nil {
		r.log.Error("super_admin sayisi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return err
	}
	if count <= 1 {
		writeError(w, http.StatusBadRequest, errLastSuperAdmin)
		return errLastSuperAdmin
	}
	return nil
}

func (r *Router) handleDeleteUser(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	claims := claimsFromContext(req.Context())
	if claims != nil && claims.UserID == id {
		writeError(w, http.StatusBadRequest, errSelfDelete)
		return
	}

	target, err := r.findUser(req.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, errUserNotFound)
		return
	}
	if target.Role == "super_admin" {
		if err := r.rejectIfLastSuperAdmin(w, req); err != nil {
			return
		}
	}

	if err := r.db.DeleteUser(req.Context(), id); err != nil {
		r.log.Error("kullanici silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	r.log.Warn("kullanici silindi", "silen", claims.Email, "silinen_id", id)
	r.auditFromClaims(req, "user_deleted", target.Email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
