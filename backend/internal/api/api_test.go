package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dbvault/dbvault/backend/internal/api"
	"github.com/dbvault/dbvault/backend/internal/app"
	"github.com/dbvault/dbvault/backend/internal/config"
	"github.com/dbvault/dbvault/backend/internal/jobs"
	"github.com/dbvault/dbvault/backend/internal/logging"
	"github.com/dbvault/dbvault/backend/internal/worker"
)

// These tests need a disposable metadata database and Redis:
//
//	DBVAULT_TEST_DATABASE_URL=postgres://...  (DBVault's own schema is created here)
//	DBVAULT_TEST_REDIS_URL=redis://localhost:6379/9
//	DBVAULT_TEST_SOURCE_URL=postgres://...    (optional: server to back up, for the job flow test)

type env struct {
	srv *httptest.Server
	app *app.App
}

var shared *env

// instanceAdminEmail is unique per run because the test database persists.
var instanceAdminEmail = uniqueEmail("instance-admin")

func setup(t *testing.T) *env {
	t.Helper()
	dbURL, redisURL := os.Getenv("DBVAULT_TEST_DATABASE_URL"), os.Getenv("DBVAULT_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("DBVAULT_TEST_DATABASE_URL and DBVAULT_TEST_REDIS_URL not set")
	}
	if shared != nil {
		return shared
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i * 7)
	}
	cfg := &config.Config{
		Env: "test", LogLevel: "warn", AppURL: "http://dbvault.test", AllowRegistration: true,
		DatabaseURL: dbURL, RedisURL: redisURL, MigrateOnStart: true,
		AuthSecret: []byte("test-auth-secret-0123456789abcdef-test"), EncryptionKey: key,
		LocalStorageRoot: t.TempDir(), WorkDir: t.TempDir(), WorkerConcurrency: 2, VerifyUploadedData: true,
		VerifyMode: config.VerifyModeDisabled, RateLimitAuthPerMinute: 1000, RateLimitAPIPerMinute: 10000,
		SMTP: config.SMTPConfig{TLSMode: "none"}, AllowPrivateNetworkTargets: true,
		InstanceAdminEmails: []string{instanceAdminEmail},
	}
	log := logging.NewWithWriter(io.Discard, "error", "test")
	a, err := app.New(context.Background(), cfg, log, app.RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	a.ConfigureSandbox(context.Background())
	_ = a.Redis.Del(context.Background(), jobs.QueueKey).Err()
	w := &worker.Worker{ID: "test-worker", Pool: a.Pool, Queue: a.Queue, Redis: a.Redis, Concurrency: 2, GracePeriod: time.Second, Log: log,
		Capabilities: map[string]any{"verify_mode": "disabled", "verify_available": false},
		Handlers: map[string]worker.Handler{
			jobs.TypeBackup: a.Backups.ExecuteBackup, jobs.TypeCleanup: a.Backups.ExecuteCleanup,
			jobs.TypeRestore: a.Restores.ExecuteRestore, jobs.TypeVerification: a.Restores.ExecuteVerification,
			jobs.TypeNotification: a.Notifications.Deliver,
		}}
	go w.Run(context.Background())
	shared = &env{srv: httptest.NewServer(api.NewRouter(a)), app: a}
	return shared
}

type client struct {
	t     *testing.T
	base  string
	http  *http.Client
	token string // API token (bearer) instead of cookies
	org   string
}

func newClient(t *testing.T, e *env) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: e.srv.URL, http: &http.Client{Jar: jar}}
}

func (c *client) csrf() string {
	u, _ := url.Parse(c.base)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "dbvault_csrf" {
			return ck.Value
		}
	}
	return ""
}

type resp struct {
	Status int
	Body   map[string]any
	Raw    []byte
}

func (r resp) data() map[string]any {
	d, _ := r.Body["data"].(map[string]any)
	return d
}

func (r resp) list() []any {
	d, _ := r.Body["data"].([]any)
	return d
}

func (r resp) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (c *client) do(method, path string, body any, opts ...func(*http.Request)) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", c.csrf())
	}
	if c.org != "" {
		req.Header.Set("X-DBVault-Org", c.org)
	}
	for _, o := range opts {
		o(req)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Raw: raw}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

func register(t *testing.T, c *client, name string) (email, orgID string) {
	t.Helper()
	email = uniqueEmail(strings.ToLower(name))
	r := c.do("POST", "/api/auth/register", map[string]string{"name": name, "email": email, "password": "correct-horse-battery"})
	if r.Status != http.StatusCreated {
		t.Fatalf("register: %d %s", r.Status, r.Raw)
	}
	return email, r.data()["organization_id"].(string)
}

func TestAuthLifecycle(t *testing.T) {
	e := setup(t)
	c := newClient(t, e)
	email, _ := register(t, c, "Ada")

	if r := c.do("GET", "/api/me", nil); r.Status != 200 || r.data()["user"].(map[string]any)["email"] != email {
		t.Fatalf("me: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/api/auth/logout", nil); r.Status != 204 {
		t.Fatalf("logout: %d", r.Status)
	}
	if r := c.do("GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("session must be revoked after logout, got %d", r.Status)
	}
	if r := c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": "wrong-password-123"}); r.Status != 401 {
		t.Fatalf("wrong password: %d", r.Status)
	}
	if r := c.do("POST", "/api/auth/login", map[string]string{"email": "nobody-" + email, "password": "x"}); r.Status != 401 {
		t.Fatalf("unknown email: %d", r.Status)
	}
	if r := c.do("POST", "/api/auth/login", map[string]string{"email": strings.ToUpper(email), "password": "correct-horse-battery"}); r.Status != 200 {
		t.Fatalf("login (case-insensitive email): %d %s", r.Status, r.Raw)
	}
	if r := newClient(t, e).do("POST", "/api/auth/register", map[string]string{"name": "Dup", "email": email, "password": "correct-horse-battery"}); r.Status != 422 {
		t.Fatalf("duplicate email: %d", r.Status)
	}
	if r := newClient(t, e).do("POST", "/api/auth/register", map[string]string{"name": "Weak", "email": uniqueEmail("weak"), "password": "short"}); r.errCode() != "validation_failed" {
		t.Fatalf("weak password: %s", r.Raw)
	}
	// Passwords are stored as argon2id hashes only.
	var hash string
	if err := e.app.Pool.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "correct-horse") {
		t.Fatalf("password not hashed: %s", hash)
	}
}

func TestCSRFRequiredForCookieSessions(t *testing.T) {
	e := setup(t)
	c := newClient(t, e)
	register(t, c, "Csrf")
	r := c.do("POST", "/api/organizations", map[string]string{"name": "x"}, func(r *http.Request) { r.Header.Del("X-CSRF-Token") })
	if r.Status != 403 {
		t.Fatalf("missing CSRF token must be rejected, got %d", r.Status)
	}
	r = c.do("POST", "/api/organizations", map[string]string{"name": "x"}, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })
	if r.Status != 403 {
		t.Fatalf("foreign origin must be rejected, got %d", r.Status)
	}
	if r := c.do("POST", "/api/organizations", map[string]string{"name": "Second org"}); r.Status != 201 {
		t.Fatalf("valid request rejected: %d %s", r.Status, r.Raw)
	}
}

func sampleDatabase(name string) map[string]any {
	return map[string]any{"name": name, "host": "127.0.0.1", "port": 1, "database": "app", "username": "app", "password": "super-secret-pw", "ssl_mode": "disable"}
}

func TestTenantIsolationAndSecrets(t *testing.T) {
	e := setup(t)
	a := newClient(t, e)
	_, orgA := register(t, a, "Alice")
	r := a.do("POST", "/api/databases", sampleDatabase("prod"))
	if r.Status != 201 {
		t.Fatalf("create database: %d %s", r.Status, r.Raw)
	}
	dbID := r.data()["database"].(map[string]any)["id"].(string)
	for _, path := range []string{"/api/databases", "/api/databases/" + dbID} {
		if raw := a.do("GET", path, nil).Raw; bytes.Contains(raw, []byte("super-secret-pw")) || bytes.Contains(raw, []byte("password_encrypted")) {
			t.Fatalf("%s exposes the password: %s", path, raw)
		}
	}
	// The stored value is sealed.
	var sealed string
	_ = e.app.Pool.QueryRow(context.Background(), `SELECT password_encrypted FROM databases WHERE id = $1`, dbID).Scan(&sealed)
	if strings.Contains(sealed, "super-secret") || !strings.HasPrefix(sealed, "v1.") {
		t.Fatalf("password not encrypted at rest: %q", sealed)
	}

	b := newClient(t, e)
	register(t, b, "Bob")
	if r := b.do("GET", "/api/databases/"+dbID, nil); r.Status != 404 {
		t.Fatalf("other tenant can read database: %d", r.Status)
	}
	b.org = orgA
	if r := b.do("GET", "/api/databases", nil); r.Status != 404 {
		t.Fatalf("non-member can select organization: %d", r.Status)
	}
	if r := a.do("GET", "/api/databases/not-a-uuid", nil); r.Status != 404 {
		t.Fatalf("malformed id: %d", r.Status)
	}
}

func TestInstanceAdminIsReadOnlyAndHidden(t *testing.T) {
	e := setup(t)
	owner := newClient(t, e)
	_, orgID := register(t, owner, "Tenant")
	if r := owner.do("POST", "/api/databases", sampleDatabase("tenant-db")); r.Status != 201 {
		t.Fatalf("create database: %d %s", r.Status, r.Raw)
	}

	// Regular users can't see the admin area exists.
	if r := owner.do("GET", "/api/admin/organizations", nil); r.Status != 404 {
		t.Fatalf("non-admin reached admin API: %d", r.Status)
	}
	if r := owner.do("GET", "/api/me", nil); r.data()["is_instance_admin"] != false {
		t.Fatalf("non-admin flagged as admin: %s", r.Raw)
	}

	adm := newClient(t, e)
	if r := adm.do("POST", "/api/auth/register", map[string]string{"name": "Root", "email": strings.ToUpper(instanceAdminEmail), "password": "correct-horse-battery"}); r.Status != 201 {
		t.Fatalf("register admin: %d %s", r.Status, r.Raw)
	}
	if r := adm.do("GET", "/api/me", nil); r.data()["is_instance_admin"] != true {
		t.Fatalf("admin not flagged: %s", r.Raw)
	}
	if r := adm.do("GET", "/api/admin/overview", nil); r.Status != 200 {
		t.Fatalf("overview: %d %s", r.Status, r.Raw)
	}
	found := false
	for _, o := range adm.do("GET", "/api/admin/organizations", nil).list() {
		found = found || o.(map[string]any)["id"] == orgID
	}
	if !found {
		t.Fatal("admin organization list is missing a tenant")
	}
	r := adm.do("GET", "/api/admin/organizations/"+orgID, nil)
	if r.Status != 200 || len(r.data()["databases"].([]any)) != 1 {
		t.Fatalf("organization detail: %d %s", r.Status, r.Raw)
	}
	for _, secret := range []string{"super-secret-pw", "password_encrypted", "credentials_encrypted", `"username"`} {
		if bytes.Contains(r.Raw, []byte(secret)) {
			t.Fatalf("admin detail exposes %s: %s", secret, r.Raw)
		}
	}
	if r := adm.do("GET", "/api/admin/users", nil); r.Status != 200 || len(r.list()) < 2 {
		t.Fatalf("users: %d %s", r.Status, r.Raw)
	}

	// The tenant sees that an admin looked, once despite repeated views.
	adm.do("GET", "/api/admin/organizations/"+orgID, nil)
	var views int
	_ = e.app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND action = 'admin.organization_viewed'`, orgID).Scan(&views)
	if views != 1 {
		t.Fatalf("expected 1 audited view, got %d", views)
	}

	// Being an instance admin grants nothing inside other organizations.
	adm.org = orgID
	if r := adm.do("GET", "/api/databases", nil); r.Status != 404 {
		t.Fatalf("instance admin reached tenant API: %d", r.Status)
	}
	adm.org = ""
	if r := adm.do("POST", "/api/admin/organizations", map[string]string{}); r.Status != 404 && r.Status != 405 {
		t.Fatalf("admin API accepted a write: %d", r.Status)
	}

	// API tokens never carry admin rights, even for an admin's account.
	tok := newClient(t, e).do("POST", "/api/auth/token", map[string]string{"email": instanceAdminEmail, "password": "correct-horse-battery", "name": "ci"})
	cli := newClient(t, e)
	cli.token = tok.data()["token"].(string)
	if r := cli.do("GET", "/api/admin/overview", nil); r.Status != 404 {
		t.Fatalf("API token reached admin API: %d", r.Status)
	}
}

func TestRoleBasedAccess(t *testing.T) {
	e := setup(t)
	owner := newClient(t, e)
	_, orgID := register(t, owner, "Owner")
	dbID := owner.do("POST", "/api/databases", sampleDatabase("prod")).data()["database"].(map[string]any)["id"].(string)

	viewer := newClient(t, e)
	viewerEmail, _ := register(t, viewer, "Viewer")
	inv := owner.do("POST", "/api/team/invitations", map[string]string{"email": viewerEmail, "role": "viewer"})
	if inv.Status != 201 {
		t.Fatalf("invite: %d %s", inv.Status, inv.Raw)
	}
	link := inv.data()["invitation"].(map[string]any)["url"].(string)
	token := link[strings.Index(link, "token=")+6:]

	// Someone else can't accept an invitation addressed to the viewer.
	stranger := newClient(t, e)
	register(t, stranger, "Stranger")
	if r := stranger.do("POST", "/api/invitations/accept", map[string]string{"token": token}); r.Status != 403 {
		t.Fatalf("invitation accepted by wrong user: %d", r.Status)
	}
	if r := viewer.do("POST", "/api/invitations/accept", map[string]string{"token": token}); r.Status != 200 {
		t.Fatalf("accept: %d %s", r.Status, r.Raw)
	}
	viewer.org = orgID

	if r := viewer.do("GET", "/api/databases", nil); r.Status != 200 || len(r.list()) != 1 {
		t.Fatalf("viewer read: %d %s", r.Status, r.Raw)
	}
	forbidden := []struct{ method, path string }{
		{"POST", "/api/databases"},
		{"PATCH", "/api/databases/" + dbID},
		{"DELETE", "/api/databases/" + dbID},
		{"POST", "/api/backups"},
		{"POST", "/api/storage"},
		{"POST", "/api/schedules"},
		{"POST", "/api/restores"},
		{"POST", "/api/team/invitations"},
		{"POST", "/api/organization/recovery-key"},
	}
	for _, f := range forbidden {
		body := map[string]any{}
		if f.path == "/api/backups" {
			body = map[string]any{"database_id": dbID}
		}
		if r := viewer.do(f.method, f.path, body); r.Status != 403 {
			t.Errorf("viewer %s %s: got %d, want 403", f.method, f.path, r.Status)
		}
	}
	// Owner promotes the viewer to member: backups are now allowed (and fail
	// only for lack of storage).
	var viewerID string
	_ = e.app.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, viewerEmail).Scan(&viewerID)
	if r := owner.do("PATCH", "/api/team/members/"+viewerID, map[string]string{"role": "member"}); r.Status != 204 {
		t.Fatalf("change role: %d %s", r.Status, r.Raw)
	}
	if r := viewer.do("POST", "/api/backups", map[string]any{"database_id": dbID}); r.errCode() != "no_storage" {
		t.Fatalf("member backup: %d %s", r.Status, r.Raw)
	}
	// The last owner can't be demoted.
	var ownerID string
	_ = e.app.Pool.QueryRow(context.Background(), `SELECT user_id FROM organization_members WHERE organization_id = $1 AND role = 'owner'`, orgID).Scan(&ownerID)
	if r := owner.do("PATCH", "/api/team/members/"+ownerID, map[string]string{"role": "admin"}); r.Status != 409 {
		t.Fatalf("demoting the last owner: %d", r.Status)
	}
	// Every action above is in the audit log.
	logs := owner.do("GET", "/api/audit-logs?limit=100", nil)
	actions := map[string]bool{}
	for _, l := range logs.list() {
		actions[l.(map[string]any)["action"].(string)] = true
	}
	for _, want := range []string{"database.created", "member.invited", "member.joined", "member.role_changed"} {
		if !actions[want] {
			t.Errorf("audit log missing %s", want)
		}
	}
}

func TestAPITokens(t *testing.T) {
	e := setup(t)
	c := newClient(t, e)
	email, _ := register(t, c, "Cli")
	anon := newClient(t, e)
	r := anon.do("POST", "/api/auth/token", map[string]string{"email": email, "password": "correct-horse-battery", "name": "laptop"})
	if r.Status != 201 {
		t.Fatalf("token: %d %s", r.Status, r.Raw)
	}
	cli := newClient(t, e)
	cli.token = r.data()["token"].(string)
	if r := cli.do("POST", "/api/databases", sampleDatabase("from-cli")); r.Status != 201 {
		t.Fatalf("bearer create (no CSRF needed): %d %s", r.Status, r.Raw)
	}
	cli.token = "dbv_invalid"
	if r := cli.do("GET", "/api/databases", nil); r.Status != 401 {
		t.Fatalf("invalid token: %d", r.Status)
	}
}

func waitJob(t *testing.T, c *client, jobID string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j := c.do("GET", "/api/jobs/"+jobID, nil).data()["job"].(map[string]any)
		if s := j["status"]; s != "queued" && s != "running" {
			return j
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", jobID)
	return nil
}

// TestBackupAndRestoreThroughAPI drives the product flow the UI uses:
// add database → test → add storage → schedule → backup → verify → restore.
func TestBackupAndRestoreThroughAPI(t *testing.T) {
	e := setup(t)
	raw := os.Getenv("DBVAULT_TEST_SOURCE_URL")
	if raw == "" {
		t.Skip("DBVAULT_TEST_SOURCE_URL not set")
	}
	u, _ := url.Parse(raw)
	pw, _ := u.User.Password()
	port, _ := strconv.Atoi(u.Port())
	ctx := context.Background()

	// Create a source database with sample data.
	admin, err := pgx.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	src := fmt.Sprintf("dbvault_api_%d", time.Now().UnixNano())
	restored := src + "_r"
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+src); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP DATABASE IF EXISTS "+restored+" WITH (FORCE)")
	defer admin.Exec(ctx, "DROP DATABASE IF EXISTS "+src+" WITH (FORCE)")
	srcURL := *u
	srcURL.Path = "/" + src
	sc, err := pgx.Connect(ctx, srcURL.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Exec(ctx, `CREATE TABLE items (id serial PRIMARY KEY, name text); INSERT INTO items (name) SELECT 'item ' || g FROM generate_series(1, 4321) g;`); err != nil {
		t.Fatal(err)
	}
	sc.Close(ctx)

	c := newClient(t, e)
	register(t, c, "Flow")
	dbReq := map[string]any{"name": "production", "host": u.Hostname(), "port": port, "database": src, "username": u.User.Username(), "password": pw, "ssl_mode": "disable"}
	if r := c.do("POST", "/api/databases/test", dbReq); r.data()["ok"] != true {
		t.Fatalf("test connection: %s", r.Raw)
	}
	r := c.do("POST", "/api/databases", dbReq)
	dbID := r.data()["database"].(map[string]any)["id"].(string)

	r = c.do("POST", "/api/storage", map[string]any{"name": "disk", "type": "local", "config": map[string]any{"path": "it"}})
	if r.Status != 201 || r.data()["test"].(map[string]any)["ok"] != true {
		t.Fatalf("storage: %d %s", r.Status, r.Raw)
	}
	stID := r.data()["storage"].(map[string]any)["id"].(string)

	r = c.do("POST", "/api/schedules", map[string]any{"database_id": dbID, "storage_destination_id": stID, "preset": "daily", "timezone": "Africa/Nairobi",
		"retention": map[string]int{"daily": 7, "weekly": 4, "monthly": 6}})
	if r.Status != 201 {
		t.Fatalf("schedule: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/api/schedules", map[string]any{"database_id": dbID, "storage_destination_id": stID, "preset": "custom", "cron_expression": "* * * * *"}); r.Status != 422 {
		t.Fatalf("every-minute cron must be rejected: %d", r.Status)
	}

	r = c.do("POST", "/api/backups", map[string]any{"database_id": dbID})
	if r.Status != 202 || r.data()["status"] != "queued" || r.data()["job_id"] == "" {
		t.Fatalf("backup: %d %s", r.Status, r.Raw)
	}
	backupID := r.data()["backup_id"].(string)
	job := waitJob(t, c, r.data()["job_id"].(string), 2*time.Minute)
	if job["status"] != "completed" {
		t.Fatalf("backup job %v: %v", job["status"], job["error"])
	}
	detail := c.do("GET", "/api/backups/"+backupID, nil).data()
	b := detail["backup"].(map[string]any)
	if b["status"] != "completed" || b["encrypted"] != true || len(b["checksum_sha256"].(string)) != 64 {
		t.Fatalf("backup record: %v", b)
	}
	if len(detail["logs"].([]any)) < 5 {
		t.Fatalf("expected backup logs, got %v", detail["logs"])
	}

	// Download (raw) matches the recorded checksum header.
	res, err := c.http.Get(c.base + "/api/backups/" + backupID + "/download")
	if err != nil || res.StatusCode != 200 || res.Header.Get("X-DBVault-Checksum-SHA256") != b["checksum_sha256"] {
		t.Fatalf("download: %v %v", err, res.Status)
	}
	res.Body.Close()

	// Restore testing is disabled here: verification must say so, never "passed".
	r = c.do("POST", "/api/backups/"+backupID+"/verify", map[string]any{})
	waitJob(t, c, r.data()["job_id"].(string), time.Minute)
	b = c.do("GET", "/api/backups/"+backupID, nil).data()["backup"].(map[string]any)
	v := b["verification"].(map[string]any)
	if b["verification_status"] != "unavailable" || v["integrity"].(map[string]any)["status"] != "pass" ||
		!strings.Contains(v["restore"].(map[string]any)["message"].(string), "Restore testing unavailable in this environment") {
		t.Fatalf("verification without sandbox: %v", b)
	}

	// Destructive restore requires the typed confirmation.
	if r := c.do("POST", "/api/restores", map[string]any{"backup_id": backupID, "target_database_id": dbID, "mode": "existing", "confirmation": "restore"}); r.Status != 422 {
		t.Fatalf("restore without exact confirmation: %d", r.Status)
	}
	r = c.do("POST", "/api/restores", map[string]any{"backup_id": backupID, "target_database_id": dbID, "mode": "new", "new_database_name": restored})
	if r.Status != 202 {
		t.Fatalf("restore: %d %s", r.Status, r.Raw)
	}
	restoreID := r.data()["id"].(string)
	var status string
	for i := 0; i < 200; i++ {
		status = c.do("GET", "/api/restores/"+restoreID, nil).data()["restore"].(map[string]any)["status"].(string)
		if status == "completed" || status == "failed" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("restore status %s: %s", status, c.do("GET", "/api/restores/"+restoreID, nil).Raw)
	}
	rURL := *u
	rURL.Path = "/" + restored
	rc, err := pgx.Connect(ctx, rURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close(ctx)
	var n int
	if err := rc.QueryRow(ctx, "SELECT count(*) FROM items").Scan(&n); err != nil || n != 4321 {
		t.Fatalf("restored rows = %d (%v), want 4321", n, err)
	}

	// Dashboard reflects the work.
	stats := c.do("GET", "/api/dashboard", nil).data()["stats"].(map[string]any)
	if stats["databases"].(float64) != 1 || stats["protected_databases"].(float64) != 1 || stats["backups_today"].(float64) < 1 {
		t.Fatalf("stats: %v", stats)
	}

	// Deleting a backup removes the artifact and keeps the history row.
	if r := c.do("DELETE", "/api/backups/"+backupID, nil); r.Status != 204 {
		t.Fatalf("delete: %d %s", r.Status, r.Raw)
	}
	if s := c.do("GET", "/api/backups/"+backupID, nil).data()["backup"].(map[string]any)["status"]; s != "deleted" {
		t.Fatalf("status after delete %v", s)
	}
}
