// Package api exposes Aruzor's REST endpoints consumed by the web UI.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/auth"
	"aruzor/internal/prometheus"
	"aruzor/internal/store"
)

const RoleSuperAdmin = "super_admin"

// roleRank orders roles from least to most privileged so write endpoints can
// require "at least" a role instead of an exact match.
var roleRank = map[string]int{"viewer": 0, "editor": 1, "admin": 2, "super_admin": 3}

type Router struct {
	mux               *http.ServeMux
	db                *store.Store
	prom              *prometheus.Client
	tokens            *auth.TokenIssuer
	log               *slog.Logger
	loginLimiter      *loginLimiter
	queryLimiter      *loginLimiter
	statusPageLimiter *loginLimiter
	vapidPublicKey    string

	dsClientsMu sync.Mutex
	dsClients   map[string]*dsClientEntry // datasource id -> cached client
}

type dsClientEntry struct {
	url    string
	client *prometheus.Client
}

func NewRouter(db *store.Store, prom *prometheus.Client, tokens *auth.TokenIssuer, log *slog.Logger, allowedOrigin, vapidPublicKey string) http.Handler {
	r := &Router{
		mux:               http.NewServeMux(),
		db:                db,
		prom:              prom,
		tokens:            tokens,
		log:               log,
		loginLimiter:      newLoginLimiter(10, time.Minute),
		queryLimiter:      newLoginLimiter(120, time.Minute),
		statusPageLimiter: newLoginLimiter(30, time.Minute),
		vapidPublicKey:    vapidPublicKey,
		dsClients:         make(map[string]*dsClientEntry),
	}
	r.routes()
	return withSecurityHeaders(withBodyLimit(withLogging(withCORS(r.mux, allowedOrigin), log)))
}

func withCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, req)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

func withLogging(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, req)
		log.Info("istek",
			"method", req.Method,
			"path", req.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", req.RemoteAddr,
		)
	})
}

type claimsKey struct{}

func claimsFromContext(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(claimsKey{}).(*auth.Claims)
	return claims
}

func (r *Router) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		header := req.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			writeError(w, http.StatusUnauthorized, errMissingToken)
			return
		}
		claims, err := r.tokens.Verify(header[len(prefix):])
		if err != nil {
			r.log.Warn("yetkisiz istek", "path", req.URL.Path, "hata", err.Error())
			writeError(w, http.StatusUnauthorized, err)
			return
		}
		ctx := context.WithValue(req.Context(), claimsKey{}, claims)
		next(w, req.WithContext(ctx))
	}
}

// requireRole restricts a handler to an exact role match (used for
// super_admin-only endpoints like user/log management).
func (r *Router) requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return r.requireAuth(func(w http.ResponseWriter, req *http.Request) {
		claims := claimsFromContext(req.Context())
		if claims == nil || claims.Role != role {
			r.logForbidden(req, claims, role)
			writeError(w, http.StatusForbidden, errForbidden)
			return
		}
		next(w, req)
	})
}

// requireMinRole restricts a handler to roles at or above the given rank
// (e.g. "editor" also allows "admin" and "super_admin"). Used for write
// operations where "viewer" must stay read-only.
func (r *Router) requireMinRole(minRole string, next http.HandlerFunc) http.HandlerFunc {
	return r.requireAuth(func(w http.ResponseWriter, req *http.Request) {
		claims := claimsFromContext(req.Context())
		if claims == nil || roleRank[claims.Role] < roleRank[minRole] {
			r.logForbidden(req, claims, minRole)
			writeError(w, http.StatusForbidden, errForbidden)
			return
		}
		next(w, req)
	})
}

// logForbidden records a role-denied request as a security-relevant audit
// event (visible to super_admin in the logs page) — a user repeatedly
// hitting endpoints above their role is a privilege-escalation signal, not
// just noise for the server log.
func (r *Router) logForbidden(req *http.Request, claims *auth.Claims, requiredRole string) {
	email := ""
	var userID *string
	if claims != nil {
		email = claims.Email
		userID = &claims.UserID
	}
	r.log.Warn("yetersiz rol", "path", req.URL.Path, "gerekli_rol", requiredRole, "email", email)
	r.audit(req.Context(), userID, email, "forbidden_attempt", req.RemoteAddr)
}

func (r *Router) routes() {
	r.mux.HandleFunc("GET /api/v1/health", r.handleHealth)
	r.mux.HandleFunc("POST /api/v1/login", r.handleLogin)

	// First-run setup. Unauthenticated by necessity — there is nobody to
	// authenticate as until it has been used — and refuses to do anything
	// once an account exists.
	r.mux.HandleFunc("GET /api/v1/setup", r.handleSetupStatus)
	r.mux.HandleFunc("POST /api/v1/setup", r.handleSetup)

	// Read-only query endpoints: any authenticated role, but rate-limited
	// per user so a low-privilege account can't hammer the datasource
	// backend (heavy PromQL, or using it as an SSRF/port-scan amplifier).
	r.mux.HandleFunc("GET /api/v1/metrics/names", r.requireAuth(r.withQueryRateLimit(r.handleMetricNames)))
	r.mux.HandleFunc("GET /api/v1/query", r.requireAuth(r.withQueryRateLimit(r.handleQuery)))
	r.mux.HandleFunc("GET /api/v1/query_range", r.requireAuth(r.withQueryRateLimit(r.handleQueryRange)))
	r.mux.HandleFunc("GET /api/v1/labels/{label}/values", r.requireAuth(r.withQueryRateLimit(r.handleLabelValues)))

	// Reports which exporters this Prometheus is scraping so a fresh install
	// can suggest real panels instead of opening on an empty dashboard.
	r.mux.HandleFunc("GET /api/v1/discover", r.requireAuth(r.withQueryRateLimit(r.handleDiscover)))

	// Datasources point the backend at an arbitrary URL it will then make
	// server-side HTTP requests to (SSRF surface) — only admin+ may manage
	// them. Any authenticated role may still list them (needed to pick a
	// datasource in the query builder).
	r.mux.HandleFunc("GET /api/v1/datasources", r.requireAuth(r.handleListDatasources))
	r.mux.HandleFunc("POST /api/v1/datasources", r.requireMinRole("admin", r.handleCreateDatasource))
	r.mux.HandleFunc("DELETE /api/v1/datasources/{id}", r.requireMinRole("admin", r.handleDeleteDatasource))

	// Monitors point the backend at an arbitrary HTTP/TCP target it will
	// then connect to (same SSRF-shaped trust boundary as datasources) —
	// only admin+ may manage them, any authenticated role may view.
	r.mux.HandleFunc("GET /api/v1/monitors", r.requireAuth(r.handleListMonitors))
	r.mux.HandleFunc("POST /api/v1/monitors", r.requireMinRole("admin", r.handleCreateMonitor))
	r.mux.HandleFunc("GET /api/v1/monitors/{id}/checks", r.requireAuth(r.handleMonitorChecks))
	r.mux.HandleFunc("DELETE /api/v1/monitors/{id}", r.requireMinRole("admin", r.handleDeleteMonitor))
	r.mux.HandleFunc("POST /api/v1/monitors/{id}/snooze", r.requireMinRole("editor", r.handleSnoozeMonitor))

	// Browser push. The public key is not a secret — it is handed to every
	// browser that subscribes — so it needs no auth; subscribing/
	// unsubscribing does, since it writes to the database.
	r.mux.HandleFunc("GET /api/v1/push/vapid-key", r.handlePushVAPIDKey)
	r.mux.HandleFunc("POST /api/v1/push/subscribe", r.requireAuth(r.handlePushSubscribe))
	r.mux.HandleFunc("POST /api/v1/push/unsubscribe", r.requireAuth(r.handlePushUnsubscribe))

	// Public: only responds when a super_admin turned on the status page
	// setting, and never behind auth — it's meant to be shared externally.
	// Being unauthenticated-by-design, it needs its own IP-based rate limit
	// (the per-user query limiter doesn't apply here) so it can't be used to
	// flood the database with anonymous requests.
	r.mux.HandleFunc("GET /api/v1/status-page", r.withStatusPageRateLimit(r.handleStatusPage))

	r.mux.HandleFunc("GET /api/v1/dashboards", r.requireAuth(r.handleListDashboards))
	r.mux.HandleFunc("GET /api/v1/dashboards/{id}", r.requireAuth(r.handleGetDashboard))
	r.mux.HandleFunc("PUT /api/v1/dashboards/{id}", r.requireMinRole("editor", r.handleSaveDashboard))
	r.mux.HandleFunc("DELETE /api/v1/dashboards/{id}", r.requireMinRole("editor", r.handleDeleteDashboard))

	// Read-only share links. Creating and revoking needs editor+; the link
	// itself is the capability, so the public endpoints below take no token
	// beyond the one in the URL and can only run that dashboard's own queries.
	r.mux.HandleFunc("GET /api/v1/dashboards/{id}/share", r.requireMinRole("editor", r.handleGetShare))
	r.mux.HandleFunc("POST /api/v1/dashboards/{id}/share", r.requireMinRole("editor", r.handleCreateShare))
	r.mux.HandleFunc("DELETE /api/v1/dashboards/{id}/share", r.requireMinRole("editor", r.handleDeleteShare))
	r.mux.HandleFunc("GET /api/v1/shared/{token}", r.handleSharedDashboard)
	r.mux.HandleFunc("GET /api/v1/shared/{token}/query_range", r.handleSharedQueryRange)

	r.mux.HandleFunc("GET /api/v1/logs", r.requireRole(RoleSuperAdmin, r.handleListLogs))
	r.mux.HandleFunc("DELETE /api/v1/logs", r.requireRole(RoleSuperAdmin, r.handleDeleteLogs))

	r.mux.HandleFunc("GET /api/v1/alerts", r.requireAuth(r.handleListAlerts))
	r.mux.HandleFunc("POST /api/v1/alerts", r.requireMinRole("editor", r.handleCreateAlert))
	r.mux.HandleFunc("PATCH /api/v1/alerts/{id}", r.requireMinRole("editor", r.handleUpdateAlert))
	r.mux.HandleFunc("DELETE /api/v1/alerts/{id}", r.requireMinRole("editor", r.handleDeleteAlert))
	r.mux.HandleFunc("POST /api/v1/alerts/{id}/snooze", r.requireMinRole("editor", r.handleSnoozeAlert))
	r.mux.HandleFunc("POST /api/v1/alerts/{id}/ack", r.requireMinRole("editor", r.handleAckAlert))
	r.mux.HandleFunc("GET /api/v1/alerts/{id}/history", r.requireAuth(r.handleAlertHistory))

	r.mux.HandleFunc("GET /api/v1/users", r.requireRole(RoleSuperAdmin, r.handleListUsers))
	r.mux.HandleFunc("POST /api/v1/users", r.requireRole(RoleSuperAdmin, r.handleCreateUser))
	r.mux.HandleFunc("DELETE /api/v1/users/{id}", r.requireRole(RoleSuperAdmin, r.handleDeleteUser))

	r.mux.HandleFunc("GET /api/v1/settings", r.requireRole(RoleSuperAdmin, r.handleListSettings))
	r.mux.HandleFunc("PUT /api/v1/settings/{key}", r.requireRole(RoleSuperAdmin, r.handleUpdateSetting))
}

// withQueryRateLimit caps how often a single authenticated user may hit the
// datasource-proxying query endpoints. Must run inside requireAuth so
// claims are already in the request context. Keyed by user id rather than
// IP: these endpoints are only reachable once logged in, so the login
// rate-limiter's IP-based keying isn't the right fit here.
func (r *Router) withQueryRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		claims := claimsFromContext(req.Context())
		key := req.RemoteAddr
		if claims != nil {
			key = claims.UserID
		}
		if !r.queryLimiter.Allow(key) {
			writeError(w, http.StatusTooManyRequests, errTooManyAttempts)
			return
		}
		next(w, req)
	}
}

// withStatusPageRateLimit caps how often a single source IP may hit the
// unauthenticated public status page endpoint, so it can't be turned into
// an anonymous DB-flooding DoS vector.
func (r *Router) withStatusPageRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !r.statusPageLimiter.Allow(clientIP(req)) {
			writeError(w, http.StatusTooManyRequests, errTooManyAttempts)
			return
		}
		next(w, req)
	}
}

func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token  string `json:"token"`
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	if !r.loginLimiter.Allow(clientIP(req)) {
		r.log.Warn("giris denemesi hiz siniri asildi", "remote", req.RemoteAddr)
		// Repeated failed attempts hitting the rate limit is a brute-force
		// signal worth surfacing to super_admin, not just the server log.
		r.audit(req.Context(), nil, "", "login_rate_limited", req.RemoteAddr)
		writeError(w, http.StatusTooManyRequests, errTooManyAttempts)
		return
	}

	var body loginRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	user, err := r.db.GetUserByEmail(req.Context(), body.Email)
	if err != nil {
		r.log.Error("kullanici sorgusu basarisiz", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	if user == nil || !auth.VerifyPassword(user.PasswordHash, body.Password) {
		r.log.Warn("basarisiz giris denemesi", "email", body.Email, "remote", req.RemoteAddr)
		r.audit(req.Context(), nil, body.Email, "login_failed", req.RemoteAddr)
		writeError(w, http.StatusUnauthorized, errInvalidCredentials)
		return
	}

	token, err := r.tokens.Issue(user.ID, user.Email, user.Role)
	if err != nil {
		r.log.Error("token uretilemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	r.log.Info("basarili giris", "email", user.Email, "remote", req.RemoteAddr)

	// Flag a login from an address never seen for this account before —
	// not blocked, just surfaced for super_admin to review. Skipped on the
	// account's very first-ever login, since there's nothing to compare to.
	total, fromThisAddr, statErr := r.db.PriorSuccessfulLoginStats(req.Context(), user.ID, req.RemoteAddr)
	if statErr != nil {
		r.log.Error("giris istatistigi alinamadi", "hata", statErr.Error())
	} else if total > 0 && fromThisAddr == 0 {
		r.audit(req.Context(), &user.ID, user.Email, "login_new_ip", req.RemoteAddr)
	}

	r.audit(req.Context(), &user.ID, user.Email, "login_success", req.RemoteAddr)
	writeJSON(w, http.StatusOK, loginResponse{Token: token, UserID: user.ID, Email: user.Email, Role: user.Role})
}

// audit persists a security-relevant event; failures are only logged, never
// returned to the caller, since audit logging must not block the request.
func (r *Router) audit(ctx context.Context, userID *string, email, event, remoteAddr string) {
	if err := r.db.InsertAuditLog(ctx, uuid.NewString(), userID, email, event, remoteAddr); err != nil {
		r.log.Error("audit log yazilamadi", "hata", err.Error())
	}
}

type deleteLogsRequest struct {
	Scope  string `json:"scope"` // "all" | "user"
	UserID string `json:"userId,omitempty"`
}

func (r *Router) handleListLogs(w http.ResponseWriter, req *http.Request) {
	logs, err := r.db.ListAuditLogs(req.Context(), 500)
	if err != nil {
		r.log.Error("log listesi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (r *Router) handleDeleteLogs(w http.ResponseWriter, req *http.Request) {
	var body deleteLogsRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidBody)
		return
	}

	claims := claimsFromContext(req.Context())
	var userID *string
	switch body.Scope {
	case "all":
		userID = nil
	case "user":
		if body.UserID == "" {
			writeError(w, http.StatusBadRequest, errMissingUserID)
			return
		}
		userID = &body.UserID
	default:
		writeError(w, http.StatusBadRequest, errInvalidScope)
		return
	}

	deleted, err := r.db.DeleteAuditLogs(req.Context(), userID)
	if err != nil {
		r.log.Error("log silinemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}

	r.log.Warn("log kayitlari silindi", "silen", claims.Email, "kapsam", body.Scope, "hedef_kullanici", body.UserID, "adet", deleted)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// resolveDatasource picks the Prometheus-compatible client for a request.
// A `datasource` query param selects a specific one; without it, requests
// fall back to the "default" datasource (the one bootstrapped from
// ARUZOR_PROMETHEUS_URL), which keeps single-datasource setups working
// exactly as before this feature existed. Clients are cached per datasource
// (keyed by id+URL) so repeated queries reuse HTTP connections instead of
// paying a fresh TCP/TLS handshake on every request.
func (r *Router) resolveDatasource(ctx context.Context, req *http.Request) (*prometheus.Client, error) {
	id := req.URL.Query().Get("datasource")
	if id == "" {
		id = "default"
	}
	ds, err := r.db.GetDatasource(ctx, id)
	if err != nil {
		return nil, err
	}
	if ds == nil {
		return nil, errDatasourceNotFound
	}

	r.dsClientsMu.Lock()
	defer r.dsClientsMu.Unlock()

	if entry, ok := r.dsClients[id]; ok && entry.url == ds.URL {
		return entry.client, nil
	}
	client := prometheus.NewClient(ds.URL)
	r.dsClients[id] = &dsClientEntry{url: ds.URL, client: client}
	return client, nil
}

func (r *Router) handleMetricNames(w http.ResponseWriter, req *http.Request) {
	client, err := r.resolveDatasource(req.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	data, err := client.MetricNames(req.Context())
	if err != nil {
		r.log.Warn("veri kaynagi istegi basarisiz", "hata", err.Error())
		writeError(w, http.StatusBadGateway, errUpstreamUnavailable)
		return
	}
	writeRaw(w, http.StatusOK, data)
}

func (r *Router) handleLabelValues(w http.ResponseWriter, req *http.Request) {
	client, err := r.resolveDatasource(req.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	label := req.PathValue("label")
	data, err := client.LabelValues(req.Context(), label)
	if err != nil {
		r.log.Warn("veri kaynagi istegi basarisiz", "hata", err.Error())
		writeError(w, http.StatusBadGateway, errUpstreamUnavailable)
		return
	}
	writeRaw(w, http.StatusOK, data)
}

func (r *Router) handleQuery(w http.ResponseWriter, req *http.Request) {
	client, err := r.resolveDatasource(req.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	promQL := req.URL.Query().Get("query")
	if promQL == "" {
		writeError(w, http.StatusBadRequest, errMissingQuery)
		return
	}
	data, err := client.Query(req.Context(), promQL, time.Now())
	if err != nil {
		r.log.Warn("veri kaynagi istegi basarisiz", "hata", err.Error())
		writeError(w, http.StatusBadGateway, errUpstreamUnavailable)
		return
	}
	writeRaw(w, http.StatusOK, data)
}

func (r *Router) handleQueryRange(w http.ResponseWriter, req *http.Request) {
	client, err := r.resolveDatasource(req.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	q := req.URL.Query()
	promQL := q.Get("query")
	if promQL == "" {
		writeError(w, http.StatusBadRequest, errMissingQuery)
		return
	}

	start, err := parseUnixTime(q.Get("start"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	end, err := parseUnixTime(q.Get("end"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	step, err := time.ParseDuration(orDefault(q.Get("step"), "15s"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	data, err := client.QueryRange(req.Context(), promQL, start, end, step)
	if err != nil {
		r.log.Warn("veri kaynagi istegi basarisiz", "hata", err.Error())
		writeError(w, http.StatusBadGateway, errUpstreamUnavailable)
		return
	}
	writeRaw(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeRaw(w http.ResponseWriter, status int, data json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
