// Package store persists Aruzor's own configuration (dashboards, alert
// rules, datasources, users) in an embedded SQLite database. Time-series
// metric data itself is never stored here — it always lives in Prometheus.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite acilamadi: %w", err)
	}
	// SQLite only allows one writer at a time; letting database/sql open
	// multiple connections invites "database is locked" errors under
	// concurrent requests. A single connection serializes all access.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("sqlite baglantisi kurulamadi: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrasyon basarisiz: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Store) CreateUser(ctx context.Context, id, email, passwordHash, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES (?, ?, ?, ?)`,
		id, email, passwordHash, role,
	)
	return err
}

// CreateFirstUser creates the initial account only while the users table is
// still empty. The emptiness check lives inside the INSERT rather than in a
// separate query so two setup requests arriving together cannot both see an
// empty table and both create an administrator — this endpoint is reachable
// without authentication, so that race is a real way in, not a theoretical
// one. Reports whether a row was actually written.
func (s *Store) CreateFirstUser(ctx context.Context, id, email, passwordHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, role)
		 SELECT ?, ?, ?, 'super_admin'
		 WHERE NOT EXISTS (SELECT 1 FROM users)`,
		id, email, passwordHash,
	)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role FROM users WHERE email = ?`, email,
	)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, role, created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

type AuditLog struct {
	ID         string    `json:"id"`
	UserID     *string   `json:"userId,omitempty"`
	Email      string    `json:"email"`
	Event      string    `json:"event"`
	RemoteAddr string    `json:"remoteAddr"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (s *Store) InsertAuditLog(ctx context.Context, id string, userID *string, email, event, remoteAddr string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (id, user_id, email, event, remote_addr) VALUES (?, ?, ?, ?, ?)`,
		id, userID, email, event, remoteAddr,
	)
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, email, event, remote_addr, created_at FROM audit_logs ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		var userID sql.NullString
		if err := rows.Scan(&l.ID, &userID, &l.Email, &l.Event, &l.RemoteAddr, &l.CreatedAt); err != nil {
			return nil, err
		}
		if userID.Valid {
			l.UserID = &userID.String
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// PriorSuccessfulLoginStats reports how many login_success entries exist
// for this user overall and from this exact remote address. Used to flag a
// successful login from an address never seen for that account before —
// a lightweight "unfamiliar login" signal without needing a GeoIP lookup —
// while not flagging a user's very first-ever login (nothing to compare
// against yet).
func (s *Store) PriorSuccessfulLoginStats(ctx context.Context, userID, remoteAddr string) (total, fromThisAddr int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(CASE WHEN remote_addr = ? THEN 1 END)
		 FROM audit_logs WHERE user_id = ? AND event = 'login_success'`,
		remoteAddr, userID,
	).Scan(&total, &fromThisAddr)
	return total, fromThisAddr, err
}

// DeleteAuditLogs removes audit log entries. If userID is nil, every log
// entry is deleted; otherwise only entries belonging to that user.
func (s *Store) DeleteAuditLogs(ctx context.Context, userID *string) (int64, error) {
	var (
		res sql.Result
		err error
	)
	if userID == nil {
		res, err = s.db.ExecContext(ctx, `DELETE FROM audit_logs`)
	} else {
		res, err = s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE user_id = ?`, *userID)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type AlertRule struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	PromQL         string     `json:"promql"`
	Operator       string     `json:"operator"`
	Threshold      float64    `json:"threshold"`
	Channel        string     `json:"channel"`
	Enabled        bool       `json:"enabled"`
	LastState      string     `json:"lastState"`
	LastNotifiedAt *time.Time `json:"lastNotifiedAt,omitempty"`
	SnoozedUntil   *time.Time `json:"snoozedUntil,omitempty"`
	AckedAt        *time.Time `json:"ackedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

func (s *Store) CreateAlertRule(ctx context.Context, r AlertRule) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_rules (id, name, promql, operator, threshold, channel, enabled, last_state)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'ok')`,
		r.ID, r.Name, r.PromQL, r.Operator, r.Threshold, r.Channel, r.Enabled,
	)
	return err
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, promql, operator, threshold, channel, enabled, last_state, last_notified_at, snoozed_until, acked_at, created_at
		 FROM alert_rules ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		var lastNotified, snoozedUntil, ackedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.PromQL, &r.Operator, &r.Threshold, &r.Channel, &r.Enabled, &r.LastState, &lastNotified, &snoozedUntil, &ackedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		if lastNotified.Valid {
			r.LastNotifiedAt = &lastNotified.Time
		}
		if snoozedUntil.Valid {
			r.SnoozedUntil = &snoozedUntil.Time
		}
		if ackedAt.Valid {
			r.AckedAt = &ackedAt.Time
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// SetAlertRuleSnooze suppresses notifications for a rule until the given
// time; pass nil to clear the snooze early.
func (s *Store) SetAlertRuleSnooze(ctx context.Context, id string, until *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_rules SET snoozed_until = ? WHERE id = ?`, until, id)
	return err
}

// SetAlertRuleAck marks (or clears, with nil) a rule as acknowledged by a
// human — purely informational, doesn't change notification behavior.
func (s *Store) SetAlertRuleAck(ctx context.Context, id string, at *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_rules SET acked_at = ? WHERE id = ?`, at, id)
	return err
}

type AlertEvent struct {
	ID        string    `json:"id"`
	RuleID    string    `json:"ruleId"`
	RuleName  string    `json:"ruleName"`
	Event     string    `json:"event"` // "fired" | "resolved"
	Value     float64   `json:"value"`
	CreatedAt time.Time `json:"createdAt"`
}

// InsertAlertEvent records a state transition for a rule's history. The
// rule's name is copied in at insert time so history stays readable even
// if the rule is later renamed or deleted.
func (s *Store) InsertAlertEvent(ctx context.Context, id, ruleID, ruleName, event string, value float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_events (id, rule_id, rule_name, event, value) VALUES (?, ?, ?, ?, ?)`,
		id, ruleID, ruleName, event, value,
	)
	return err
}

func (s *Store) ListAlertEvents(ctx context.Context, ruleID string, limit int) ([]AlertEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, rule_id, rule_name, event, value, created_at FROM alert_events WHERE rule_id = ? ORDER BY created_at DESC LIMIT ?`,
		ruleID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []AlertEvent{}
	for rows.Next() {
		var e AlertEvent
		if err := rows.Scan(&e.ID, &e.RuleID, &e.RuleName, &e.Event, &e.Value, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) SetAlertRuleEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_rules SET enabled = ? WHERE id = ?`, enabled, id)
	return err
}

func (s *Store) DeleteAlertRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id)
	return err
}

// UpdateAlertRuleState also clears any acknowledgement — a new state
// transition means a new incident, which should require a fresh ack.
func (s *Store) UpdateAlertRuleState(ctx context.Context, id, state string, notifiedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE alert_rules SET last_state = ?, last_notified_at = ?, acked_at = NULL WHERE id = ?`,
		state, notifiedAt, id,
	)
	return err
}

type Dashboard struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Definition string    `json:"-"` // raw JSON text; exposed via json.RawMessage in the API layer
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (s *Store) GetDashboard(ctx context.Context, id string) (*Dashboard, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, definition, updated_at FROM dashboards WHERE id = ?`, id,
	)
	var d Dashboard
	if err := row.Scan(&d.ID, &d.Title, &d.Definition, &d.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (s *Store) UpsertDashboard(ctx context.Context, id, title, definition string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dashboards (id, title, definition) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET title = excluded.title, definition = excluded.definition, updated_at = CURRENT_TIMESTAMP
	`, id, title, definition)
	return err
}

// ListDashboards returns dashboard metadata only (no definition — that can
// be large and is fetched separately per-dashboard) for the dashboard
// switcher UI, newest-updated first.
func (s *Store) ListDashboards(ctx context.Context) ([]Dashboard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, updated_at FROM dashboards ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dashboards := []Dashboard{}
	for rows.Next() {
		var d Dashboard
		if err := rows.Scan(&d.ID, &d.Title, &d.UpdatedAt); err != nil {
			return nil, err
		}
		dashboards = append(dashboards, d)
	}
	return dashboards, rows.Err()
}

func (s *Store) CountDashboards(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dashboards`).Scan(&count)
	return count, err
}

func (s *Store) DeleteDashboard(ctx context.Context, id string) error {
	// The share link goes with it. Left behind, the row would silently
	// re-activate if a new dashboard were ever created with the same id.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM dashboard_shares WHERE dashboard_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM dashboards WHERE id = ?`, id)
	return err
}

type Datasource struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) CreateDatasource(ctx context.Context, d Datasource) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO datasources (id, name, url, type) VALUES (?, ?, ?, ?)`,
		d.ID, d.Name, d.URL, d.Type,
	)
	return err
}

func (s *Store) GetDatasource(ctx context.Context, id string) (*Datasource, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, url, type, created_at FROM datasources WHERE id = ?`, id)
	var d Datasource
	if err := row.Scan(&d.ID, &d.Name, &d.URL, &d.Type, &d.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListDatasources(ctx context.Context) ([]Datasource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, url, type, created_at FROM datasources ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	datasources := []Datasource{}
	for rows.Next() {
		var d Datasource
		if err := rows.Scan(&d.ID, &d.Name, &d.URL, &d.Type, &d.CreatedAt); err != nil {
			return nil, err
		}
		datasources = append(datasources, d)
	}
	return datasources, rows.Err()
}

func (s *Store) CountDatasources(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM datasources`).Scan(&count)
	return count, err
}

func (s *Store) DeleteDatasource(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM datasources WHERE id = ?`, id)
	return err
}

type Monitor struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"` // "http" | "tcp"
	Target          string     `json:"target"`
	IntervalSeconds int        `json:"intervalSeconds"`
	LastOK          *bool      `json:"lastOk,omitempty"`
	LastCheckedAt   *time.Time `json:"lastCheckedAt,omitempty"`
	LastLatencyMs   *int       `json:"lastLatencyMs,omitempty"`
	CertExpiresAt   *time.Time `json:"certExpiresAt,omitempty"`
	// The expiry date a warning has already been sent for. Storing the date
	// rather than a boolean is what makes a renewal reset the warning by
	// itself: the new certificate has a different expiry, so it no longer
	// matches what was warned about. Internal state, never sent to clients.
	CertWarnedFor *time.Time `json:"-"`
	CreatedAt     time.Time  `json:"createdAt"`

	// AlertState is what the chat has already been told: "ok" or "down".
	// It is deliberately separate from LastOK, which is the raw result of
	// the most recent check — one failed check is not yet an outage, and
	// without this the same outage would be announced on every tick.
	AlertState string `json:"-"`
	// ConsecutiveFailures counts failures since the last success, so a
	// single blip can be told apart from something that is actually down.
	ConsecutiveFailures int `json:"-"`
	// FailingSince is when the current run of failures started. Duration is
	// what separates a stall from an outage, and a count cannot express it:
	// two failures mean two minutes on one monitor and two hours on another.
	FailingSince *time.Time `json:"failingSince,omitempty"`

	// SnoozedUntil suppresses down notifications for planned maintenance —
	// the check keeps running and history keeps recording, only the alert
	// is held back. Mirrors alert_rules.snoozed_until.
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`

	// Custom HTTP check config — http monitors only. Empty on tcp monitors
	// and on any monitor created before this existed, in which case the
	// checker falls back to a plain GET with the default 2xx-3xx range,
	// exactly as it always has.
	Method             string `json:"method,omitempty"`
	RequestBody        string `json:"requestBody,omitempty"`
	ContentType        string `json:"contentType,omitempty"`
	ExpectedStatus     string `json:"expectedStatus,omitempty"`
	ExpectBodyContains string `json:"expectBodyContains,omitempty"`
}

func (s *Store) CreateMonitor(ctx context.Context, m Monitor) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO monitors (id, name, type, target, interval_seconds, method, request_body, content_type, expected_status, expect_body_contains)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Name, m.Type, m.Target, m.IntervalSeconds,
		m.Method, m.RequestBody, m.ContentType, m.ExpectedStatus, m.ExpectBodyContains,
	)
	return err
}

func (s *Store) ListMonitors(ctx context.Context) ([]Monitor, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, type, target, interval_seconds, last_ok, last_checked_at, last_latency_ms,
		        cert_expires_at, cert_warned_for, alert_state, consecutive_failures, failing_since, snoozed_until,
		        method, request_body, content_type, expected_status, expect_body_contains, created_at
		 FROM monitors ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	monitors := []Monitor{}
	for rows.Next() {
		var m Monitor
		var lastOK sql.NullBool
		var lastCheckedAt sql.NullTime
		var lastLatency sql.NullInt64
		var certExpires, certWarnedFor, failingSince, snoozedUntil sql.NullTime
		if err := rows.Scan(&m.ID, &m.Name, &m.Type, &m.Target, &m.IntervalSeconds, &lastOK, &lastCheckedAt, &lastLatency,
			&certExpires, &certWarnedFor, &m.AlertState, &m.ConsecutiveFailures, &failingSince, &snoozedUntil,
			&m.Method, &m.RequestBody, &m.ContentType, &m.ExpectedStatus, &m.ExpectBodyContains, &m.CreatedAt); err != nil {
			return nil, err
		}
		if lastOK.Valid {
			m.LastOK = &lastOK.Bool
		}
		if lastCheckedAt.Valid {
			m.LastCheckedAt = &lastCheckedAt.Time
		}
		if lastLatency.Valid {
			v := int(lastLatency.Int64)
			m.LastLatencyMs = &v
		}
		if certExpires.Valid {
			m.CertExpiresAt = &certExpires.Time
		}
		if certWarnedFor.Valid {
			m.CertWarnedFor = &certWarnedFor.Time
		}
		if failingSince.Valid {
			m.FailingSince = &failingSince.Time
		}
		if snoozedUntil.Valid {
			m.SnoozedUntil = &snoozedUntil.Time
		}
		monitors = append(monitors, m)
	}
	return monitors, rows.Err()
}

func (s *Store) DeleteMonitor(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitors WHERE id = ?`, id)
	return err
}

// SetMonitorSnooze suppresses down notifications for this monitor until the
// given time, or clears an active snooze when until is nil. The check keeps
// running underneath — only the alert is held back — which is what makes
// this safe to use for a planned maintenance window instead of disabling
// the monitor outright.
func (s *Store) SetMonitorSnooze(ctx context.Context, id string, until *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE monitors SET snoozed_until = ? WHERE id = ?`, until, id)
	return err
}

// MonitorCheck is one recorded check. The error fields describe what the
// network stack reported and nothing more — no inference is stored here,
// so a row stays true regardless of how the UI later chooses to read it.
type MonitorCheck struct {
	ID        string `json:"id"`
	MonitorID string `json:"-"`
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latencyMs"`
	// Attempts above one means the check only passed on a retry. That is
	// not a failure, but it is not health either, and it is the signal the
	// app shows when the chat stays quiet.
	Attempts    int    `json:"attempts"`
	ErrorClass  string `json:"errorClass,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
	ConnectMs   *int   `json:"connectMs,omitempty"`
	TLSMs       *int   `json:"tlsMs,omitempty"`
	// StatusCode is the raw HTTP response code observed, when there was
	// one to observe — nil for TCP checks and for any failure that never
	// got as far as a response (DNS, refused, timeout, TLS).
	StatusCode *int      `json:"statusCode,omitempty"`
	CheckedAt  time.Time `json:"checkedAt"`
}

// RecordMonitorCheck updates the monitor's last-known status and appends a
// row to its check history (used to compute an uptime percentage).
//
// certExpiry is written only when the check actually observed one, so a
// failed check never erases the expiry date learned from the last good one.
func (s *Store) RecordMonitorCheck(ctx context.Context, c MonitorCheck, certExpiry *time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE monitors SET last_ok = ?, last_checked_at = ?, last_latency_ms = ? WHERE id = ?`,
		c.OK, c.CheckedAt, c.LatencyMs, c.MonitorID,
	); err != nil {
		return err
	}
	if certExpiry != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE monitors SET cert_expires_at = ? WHERE id = ?`, *certExpiry, c.MonitorID,
		); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO monitor_checks (id, monitor_id, ok, latency_ms, error_class, error_detail, attempts, connect_ms, tls_ms, status_code, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.MonitorID, c.OK, c.LatencyMs, c.ErrorClass, c.ErrorDetail, c.Attempts, c.ConnectMs, c.TLSMs, c.StatusCode, c.CheckedAt,
	)
	return err
}

// ListMonitorChecks returns the most recent checks for one monitor, newest
// first, for the detail view and its latency history.
func (s *Store) ListMonitorChecks(ctx context.Context, monitorID string, limit int) ([]MonitorCheck, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ok, latency_ms, error_class, error_detail, attempts, connect_ms, tls_ms, status_code, checked_at
		 FROM monitor_checks WHERE monitor_id = ? ORDER BY checked_at DESC LIMIT ?`,
		monitorID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	checks := []MonitorCheck{}
	for rows.Next() {
		var c MonitorCheck
		var connectMs, tlsMs, statusCode sql.NullInt64
		if err := rows.Scan(&c.ID, &c.OK, &c.LatencyMs, &c.ErrorClass, &c.ErrorDetail, &c.Attempts, &connectMs, &tlsMs, &statusCode, &c.CheckedAt); err != nil {
			return nil, err
		}
		if connectMs.Valid {
			v := int(connectMs.Int64)
			c.ConnectMs = &v
		}
		if tlsMs.Valid {
			v := int(tlsMs.Int64)
			c.TLSMs = &v
		}
		if statusCode.Valid {
			v := int(statusCode.Int64)
			c.StatusCode = &v
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// SetMonitorAlertState persists what the chat has been told about this
// monitor and how many failures it has seen in a row. Both survive a
// restart on purpose: an in-memory counter would re-announce an ongoing
// outage every time the process came back up.
func (s *Store) SetMonitorAlertState(ctx context.Context, monitorID, state string, consecutiveFailures int, failingSince *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE monitors SET alert_state = ?, consecutive_failures = ?, failing_since = ? WHERE id = ?`,
		state, consecutiveFailures, failingSince, monitorID,
	)
	return err
}

// ListMonitorChecksSince returns every check in a time window, oldest
// first. The detail view needs a whole day to show when a service was
// struggling, which is far more rows than anyone wants to read one by one —
// the caller reduces them to buckets before they leave the server.
// SetMonitorCertWarned records which certificate expiry a warning has
// already gone out for, so the same certificate is only ever announced
// once no matter how often the monitor runs.
func (s *Store) SetMonitorCertWarned(ctx context.Context, monitorID string, expiry time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE monitors SET cert_warned_for = ? WHERE id = ?`, expiry, monitorID)
	return err
}

func (s *Store) ListMonitorChecksSince(ctx context.Context, monitorID string, since time.Time) ([]MonitorCheck, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ok, latency_ms, error_class, error_detail, attempts, connect_ms, tls_ms, status_code, checked_at
		 FROM monitor_checks WHERE monitor_id = ? AND checked_at >= ? ORDER BY checked_at ASC`,
		monitorID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	checks := []MonitorCheck{}
	for rows.Next() {
		var c MonitorCheck
		var connectMs, tlsMs, statusCode sql.NullInt64
		if err := rows.Scan(&c.ID, &c.OK, &c.LatencyMs, &c.ErrorClass, &c.ErrorDetail, &c.Attempts, &connectMs, &tlsMs, &statusCode, &c.CheckedAt); err != nil {
			return nil, err
		}
		if connectMs.Valid {
			v := int(connectMs.Int64)
			c.ConnectMs = &v
		}
		if tlsMs.Valid {
			v := int(tlsMs.Int64)
			c.TLSMs = &v
		}
		if statusCode.Valid {
			v := int(statusCode.Int64)
			c.StatusCode = &v
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// UptimePercent returns the fraction (0-100) of checks that were OK since
// the given time. Returns ok=false when there's no history yet to compute
// a meaningful percentage from.
func (s *Store) UptimePercent(ctx context.Context, monitorID string, since time.Time) (percent float64, ok bool, err error) {
	var total, okCount int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(CASE WHEN ok THEN 1 END) FROM monitor_checks WHERE monitor_id = ? AND checked_at >= ?`,
		monitorID, since,
	).Scan(&total, &okCount)
	if err != nil || total == 0 {
		return 0, false, err
	}
	return float64(okCount) / float64(total) * 100, true, nil
}

// PruneOldMonitorChecks deletes check history older than the given time,
// keeping the monitor_checks table from growing without bound.
func (s *Store) PruneOldMonitorChecks(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM monitor_checks WHERE checked_at < ?`, before)
	return err
}

// UptimeSummary is the uptime percentage over several standard windows —
// the numbers a status page shows instead of one figure picked for you.
// A window with no recorded checks yet (a monitor created an hour ago has
// no 90-day history) is simply omitted rather than shown as 0% or 100%,
// either of which would be a claim the data doesn't support.
type UptimeSummary struct {
	Day24h *float64 `json:"day24h,omitempty"`
	Day7   *float64 `json:"day7,omitempty"`
	Day30  *float64 `json:"day30,omitempty"`
	Day90  *float64 `json:"day90,omitempty"`
}

func (s *Store) GetUptimeSummary(ctx context.Context, monitorID string, now time.Time) (UptimeSummary, error) {
	var summary UptimeSummary
	windows := []struct {
		since time.Time
		out   **float64
	}{
		{now.Add(-24 * time.Hour), &summary.Day24h},
		{now.Add(-7 * 24 * time.Hour), &summary.Day7},
		{now.Add(-30 * 24 * time.Hour), &summary.Day30},
		{now.Add(-90 * 24 * time.Hour), &summary.Day90},
	}
	for _, win := range windows {
		pct, ok, err := s.UptimePercent(ctx, monitorID, win.since)
		if err != nil {
			return summary, err
		}
		if ok {
			v := pct
			*win.out = &v
		}
	}
	return summary, nil
}

// DailyUptime is one calendar day's worth of check results, the unit the
// public-status-page history bar is built from.
type DailyUptime struct {
	Date    string  `json:"date"` // YYYY-MM-DD, server-local
	Total   int     `json:"total"`
	Failed  int     `json:"failed"`
	Percent float64 `json:"percent"`
}

// DailyUptimeHistory returns one entry per calendar day for the last `days`
// days, oldest first, including days with zero checks (a monitor younger
// than the window, or a gap from a monitor being disabled a while). Callers
// render those as "no data" rather than skipping them, so the strip always
// has a fixed, predictable width.
func (s *Store) DailyUptimeHistory(ctx context.Context, monitorID string, days int, now time.Time) ([]DailyUptime, error) {
	since := now.AddDate(0, 0, -days+1)
	// substr, not SQLite's date(): checked_at is stored as this driver's
	// RFC3339 text with a numeric zone offset ("...+03:00"), a shape
	// SQLite's own date functions fail to parse — date() silently returns
	// NULL rather than an error. The first 10 characters are always the
	// calendar date in the server's local zone, which is what "day" means
	// here (matching how every other check timestamp in Aruzor is read).
	rows, err := s.db.QueryContext(ctx,
		`SELECT substr(checked_at, 1, 10) AS day, COUNT(*), COUNT(CASE WHEN NOT ok THEN 1 END)
		 FROM monitor_checks WHERE monitor_id = ? AND checked_at >= ?
		 GROUP BY day`,
		monitorID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := make(map[string]DailyUptime)
	for rows.Next() {
		var d DailyUptime
		if err := rows.Scan(&d.Date, &d.Total, &d.Failed); err != nil {
			return nil, err
		}
		if d.Total > 0 {
			d.Percent = float64(d.Total-d.Failed) / float64(d.Total) * 100
		}
		byDay[d.Date] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Filled day-by-day rather than trusting SQL to have produced a
	// contiguous range — a day with no checks never gets a GROUP BY row at
	// all, and the strip needs one anyway to keep every day's slot in place.
	out := make([]DailyUptime, 0, days)
	for i := 0; i < days; i++ {
		day := since.AddDate(0, 0, i).Format("2006-01-02")
		if d, ok := byDay[day]; ok {
			out = append(out, d)
		} else {
			out = append(out, DailyUptime{Date: day})
		}
	}
	return out, nil
}

// PushSubscription is one browser's Web Push registration — everything
// needed to encrypt and address a notification to it.
type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// UpsertPushSubscription records or refreshes a subscription. Endpoint is
// the primary key, so re-subscribing the same browser (permission
// re-granted, service worker updated) replaces the keys in place instead of
// piling up duplicates that would each get their own copy of every alert.
func (s *Store) UpsertPushSubscription(ctx context.Context, endpoint, p256dh, auth string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO push_subscriptions (endpoint, p256dh, auth) VALUES (?, ?, ?)
		 ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth`,
		endpoint, p256dh, auth,
	)
	return err
}

func (s *Store) DeletePushSubscription(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

func (s *Store) ListPushSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT endpoint, p256dh, auth FROM push_subscriptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subs := []PushSubscription{}
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.Endpoint, &p.P256dh, &p.Auth); err != nil {
			return nil, err
		}
		subs = append(subs, p)
	}
	return subs, rows.Err()
}

// Settings are simple super_admin-controlled feature toggles (e.g. "is the
// public status page enabled"), stored as plain key/value text so new flags
// don't need a schema migration.

func (s *Store) ListSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// --- Dashboard share links ------------------------------------------------

// CreateShare replaces any existing link for this dashboard. Sharing a
// dashboard that is already shared should hand back a *new* token rather
// than the old one: the usual reason to re-share is that the previous link
// went somewhere it should not have.
func (s *Store) CreateShare(ctx context.Context, token, dashboardID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dashboard_shares (token, dashboard_id) VALUES (?, ?)
		 ON CONFLICT(dashboard_id) DO UPDATE SET token = excluded.token, created_at = CURRENT_TIMESTAMP`,
		token, dashboardID,
	)
	return err
}

func (s *Store) GetShareToken(ctx context.Context, dashboardID string) (string, bool, error) {
	var token string
	err := s.db.QueryRowContext(ctx,
		`SELECT token FROM dashboard_shares WHERE dashboard_id = ?`, dashboardID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (s *Store) GetSharedDashboardID(ctx context.Context, token string) (string, bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT dashboard_id FROM dashboard_shares WHERE token = ?`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func (s *Store) DeleteShare(ctx context.Context, dashboardID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dashboard_shares WHERE dashboard_id = ?`, dashboardID)
	return err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS datasources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'prometheus',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS dashboards (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		definition TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS alert_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		promql TEXT NOT NULL,
		operator TEXT NOT NULL,
		threshold REAL NOT NULL,
		channel TEXT NOT NULL DEFAULT 'telegram',
		enabled BOOLEAN NOT NULL DEFAULT 1,
		last_state TEXT NOT NULL DEFAULT 'ok',
		last_notified_at DATETIME,
		snoozed_until DATETIME,
		acked_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS alert_events (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL,
		rule_name TEXT NOT NULL,
		event TEXT NOT NULL,
		value REAL NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'viewer',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		email TEXT NOT NULL,
		event TEXT NOT NULL,
		remote_addr TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	-- One share link per dashboard. The token is the whole capability, so
	-- revoking is a delete and re-sharing mints a new one — an old link can
	-- never be resurrected by sharing again.
	CREATE TABLE IF NOT EXISTS dashboard_shares (
		token TEXT PRIMARY KEY,
		dashboard_id TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS monitors (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		target TEXT NOT NULL,
		interval_seconds INTEGER NOT NULL DEFAULT 60,
		last_ok BOOLEAN,
		last_checked_at DATETIME,
		last_latency_ms INTEGER,
		cert_expires_at DATETIME,
		alert_state TEXT NOT NULL DEFAULT 'ok',
		failing_since DATETIME,
		consecutive_failures INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS monitor_checks (
		id TEXT PRIMARY KEY,
		monitor_id TEXT NOT NULL,
		ok BOOLEAN NOT NULL,
		latency_ms INTEGER NOT NULL,
		error_class TEXT NOT NULL DEFAULT '',
		error_detail TEXT NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 1,
		connect_ms INTEGER,
		tls_ms INTEGER,
		checked_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- One row per browser/device that opted into push notifications. Endpoint
	-- is the primary key rather than a generated id: it's what the browser
	-- hands back on every subscribe call, so re-subscribing the same device
	-- (a common case — permission re-granted, service worker updated) is a
	-- plain upsert instead of an accumulating pile of duplicates.
	CREATE TABLE IF NOT EXISTS push_subscriptions (
		endpoint TEXT PRIMARY KEY,
		p256dh TEXT NOT NULL,
		auth TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// alert_rules existed before snooze/ack were added — CREATE TABLE IF NOT
	// EXISTS above won't retrofit columns onto an already-existing table, so
	// add them here. SQLite has no "ADD COLUMN IF NOT EXISTS", so a
	// "duplicate column name" error (already migrated) is expected and
	// ignored; any other error is real.
	for _, stmt := range []string{
		`ALTER TABLE alert_rules ADD COLUMN snoozed_until DATETIME`,
		`ALTER TABLE alert_rules ADD COLUMN acked_at DATETIME`,
		`ALTER TABLE monitors ADD COLUMN alert_state TEXT NOT NULL DEFAULT 'ok'`,
		`ALTER TABLE monitors ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE monitors ADD COLUMN cert_expires_at DATETIME`,
		`ALTER TABLE monitors ADD COLUMN failing_since DATETIME`,
		`ALTER TABLE monitor_checks ADD COLUMN error_class TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitor_checks ADD COLUMN error_detail TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitor_checks ADD COLUMN connect_ms INTEGER`,
		`ALTER TABLE monitor_checks ADD COLUMN tls_ms INTEGER`,
		`ALTER TABLE monitor_checks ADD COLUMN attempts INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE monitors ADD COLUMN snoozed_until DATETIME`,
		`ALTER TABLE monitors ADD COLUMN method TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitors ADD COLUMN request_body TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitors ADD COLUMN content_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitors ADD COLUMN expected_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitors ADD COLUMN expect_body_contains TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE monitor_checks ADD COLUMN status_code INTEGER`,
		`ALTER TABLE monitors ADD COLUMN cert_warned_for DATETIME`,
	} {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}

	// Traffic analytics keeps its own tables in store/traffic.go — they are
	// a self-contained subsystem with their own retention rules, and mixing
	// them into the schema literal above would bury that.
	return s.migrateTraffic()
}
