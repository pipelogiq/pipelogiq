package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/config"
	"pipelogiq/internal/pro"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

// Applications used across the scoping tests: the caller belongs to "own" only.
const (
	ownApplicationID   = 1
	otherApplicationID = 2
	scopedUserID       = 10
	strangerUserID     = 11
)

func TestApplicationScope_DeniesCrossApplicationPipelineReads(t *testing.T) {
	fx := setupScopeTest(t)

	for _, path := range []string{
		fmt.Sprintf("/pipelines/%d", fx.otherPipelineID),
		fmt.Sprintf("/pipelines/%d/stages", fx.otherPipelineID),
		fmt.Sprintf("/pipelines/%d/context", fx.otherPipelineID),
		fmt.Sprintf("/pipelines/logs/%d", fx.otherPipelineID),
	} {
		recorder := fx.do(t, scopedUserID, http.MethodGet, path, nil)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d (body=%s)", path, recorder.Code, http.StatusNotFound, recorder.Body.String())
		}
	}
}

func TestApplicationScope_AllowsOwnApplicationPipelineReads(t *testing.T) {
	fx := setupScopeTest(t)

	path := fmt.Sprintf("/pipelines/%d", fx.ownPipelineID)
	recorder := fx.do(t, scopedUserID, http.MethodGet, path, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want %d (body=%s)", path, recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestApplicationScope_ListReturnsOnlyOwnApplications(t *testing.T) {
	fx := setupScopeTest(t)

	recorder := fx.do(t, scopedUserID, http.MethodGet, "/pipelines?pageSize=50", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var page types.PagedResult[types.PipelineResponse]
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if page.TotalCount != 1 {
		t.Fatalf("totalCount = %d, want 1", page.TotalCount)
	}
	for _, item := range page.Items {
		if item.ID != fx.ownPipelineID {
			t.Errorf("list leaked pipeline %d from another application", item.ID)
		}
	}
}

func TestApplicationScope_ListRejectsForeignApplicationFilter(t *testing.T) {
	fx := setupScopeTest(t)

	path := fmt.Sprintf("/pipelines?applicationId=%d", otherApplicationID)
	recorder := fx.do(t, scopedUserID, http.MethodGet, path, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestApplicationScope_UserWithoutMembershipSeesNothing(t *testing.T) {
	fx := setupScopeTest(t)

	recorder := fx.do(t, strangerUserID, http.MethodGet, "/pipelines", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var page types.PagedResult[types.PipelineResponse]
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.TotalCount != 0 || len(page.Items) != 0 {
		t.Fatalf("user without membership saw %d pipelines", len(page.Items))
	}

	// An explicit filter for someone else's application is refused, not answered empty.
	filtered := fx.do(t, strangerUserID, http.MethodGet, fmt.Sprintf("/pipelines?applicationId=%d", ownApplicationID), nil)
	if filtered.Code != http.StatusNotFound {
		t.Fatalf("filtered status = %d, want %d", filtered.Code, http.StatusNotFound)
	}
}

func TestApplicationScope_DeniesCrossApplicationMutations(t *testing.T) {
	fx := setupScopeTest(t)

	cases := []struct {
		name string
		path string
		body any
	}{
		{"pause", fmt.Sprintf("/pipelines/%d/pause", fx.otherPipelineID), nil},
		{"resume", fmt.Sprintf("/pipelines/%d/resume", fx.otherPipelineID), nil},
		{"rerunStage", "/pipelines/rerunStage", map[string]any{"stageId": fx.otherStageID}},
		{"skipStage", "/pipelines/skipStage", map[string]any{"stageId": fx.otherStageID}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := fx.do(t, scopedUserID, http.MethodPost, tc.path, tc.body)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
			}
		})
	}

	// The foreign pipeline must be untouched.
	var status string
	if err := fx.db.Get(&status, `SELECT status FROM pipeline WHERE id = ?`, fx.otherPipelineID); err != nil {
		t.Fatalf("read pipeline status: %v", err)
	}
	if status != types.PipelineStatusRunning {
		t.Fatalf("foreign pipeline status = %q, want %q", status, types.PipelineStatusRunning)
	}
}

func TestApplicationScope_BulkActionReportsForeignTargetsAsNotFound(t *testing.T) {
	fx := setupScopeTest(t)

	body := map[string]any{
		"action":      "pause",
		"pipelineIds": []int{fx.ownPipelineID, fx.otherPipelineID},
	}
	recorder := fx.do(t, scopedUserID, http.MethodPost, "/pipelines/bulkAction", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp types.BulkPipelineActionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Requested != 2 {
		t.Fatalf("requested = %d, want 2", resp.Requested)
	}

	for _, item := range resp.Results {
		if item.ID == fx.otherPipelineID {
			if item.Success {
				t.Fatalf("bulk action executed against a foreign pipeline")
			}
			if item.Error != "not found" {
				t.Fatalf("foreign target error = %q, want %q", item.Error, "not found")
			}
		}
	}

	var status string
	if err := fx.db.Get(&status, `SELECT status FROM pipeline WHERE id = ?`, fx.otherPipelineID); err != nil {
		t.Fatalf("read pipeline status: %v", err)
	}
	if status != types.PipelineStatusRunning {
		t.Fatalf("foreign pipeline status = %q, want %q", status, types.PipelineStatusRunning)
	}
}

func TestHubBroadcast_OnlyReachesClientsScopedToTheApplication(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), []string{"http://localhost"})

	insider := &Client{hub: hub, send: make(chan []byte, 1), scope: newApplicationScope([]int{ownApplicationID}), expiresAt: time.Now().Add(time.Hour)}
	outsider := &Client{hub: hub, send: make(chan []byte, 1), scope: newApplicationScope([]int{otherApplicationID}), expiresAt: time.Now().Add(time.Hour)}
	hub.clients[insider] = struct{}{}
	hub.clients[outsider] = struct{}{}

	hub.Broadcast([]byte(fmt.Sprintf(`{"id":7,"applicationId":%d}`, ownApplicationID)))

	select {
	case <-insider.send:
	default:
		t.Fatal("client scoped to the application did not receive the update")
	}

	select {
	case msg := <-outsider.send:
		t.Fatalf("client of another application received %s", msg)
	default:
	}
}

func TestHubBroadcast_DropsPayloadWithoutApplication(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), []string{"http://localhost"})
	client := &Client{hub: hub, send: make(chan []byte, 1), scope: newApplicationScope([]int{ownApplicationID}), expiresAt: time.Now().Add(time.Hour)}
	hub.clients[client] = struct{}{}

	hub.Broadcast([]byte(`{"id":7}`))

	select {
	case msg := <-client.send:
		t.Fatalf("update without an application was delivered: %s", msg)
	default:
	}
}

// --- fixture ---

type scopeFixture struct {
	db              *sqlx.DB
	router          http.Handler
	jwtSecret       []byte
	ownPipelineID   int
	otherPipelineID int
	otherStageID    int
}

// do issues a request authenticated as the given user.
func (fx *scopeFixture) do(t *testing.T, userID int, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	token, err := generateJWT(fx.jwtSecret, userID, fmt.Sprintf("user%d@example.com", userID), "Admin", 0)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})

	recorder := httptest.NewRecorder()
	fx.router.ServeHTTP(recorder, req)
	return recorder
}

func setupScopeTest(t *testing.T) *scopeFixture {
	t.Helper()

	dsn := testDSN("scope")
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	// The scoping tests reuse the shared schema plus the membership table they need.
	schema := externalTestSchema + `
	CREATE TABLE user_application (
		user_id INTEGER NOT NULL,
		application_id INTEGER NOT NULL
	);
	CREATE TABLE "user" (
		id INTEGER PRIMARY KEY,
		first_name TEXT NOT NULL,
		last_name TEXT NULL,
		email TEXT NOT NULL,
		password TEXT NOT NULL,
		role TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		token_version INTEGER NOT NULL DEFAULT 0,
		is_active BOOLEAN NOT NULL DEFAULT 1
	);
	CREATE UNIQUE INDEX uq_user_email_normalized ON "user" (lower(trim(email)));
	CREATE TABLE audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts TIMESTAMP NOT NULL,
		user_id INTEGER NULL,
		email TEXT NULL,
		action TEXT NOT NULL,
		target TEXT NULL,
		outcome TEXT NOT NULL,
		remote_addr TEXT NULL,
		details TEXT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	seed := `
	INSERT INTO application (id, name) VALUES (1, 'own'), (2, 'other');
	INSERT INTO user_application (user_id, application_id) VALUES (10, 1);
	INSERT INTO "user" (id, first_name, email, password, role) VALUES (10, 'Scoped', 'user10@example.com', 'x', 'Admin'), (11, 'Stranger', 'user11@example.com', 'x', 'Admin');
	`
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fx := &scopeFixture{db: db, jwtSecret: []byte("scope-test-secret-0123456789abcdef")}
	fx.ownPipelineID = insertScopedPipeline(t, db, "own-pipeline", ownApplicationID)
	fx.otherPipelineID = insertScopedPipeline(t, db, "other-pipeline", otherApplicationID)
	fx.otherStageID = insertScopedStage(t, db, fx.otherPipelineID)

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	cfg := config.APIConfig{
		JWTSecret:              string(fx.jwtSecret),
		AllowedOrigins:         []string{"http://localhost"},
		HealthLivenessEndpoint: "/healthz",
		HealthReadyEndpoint:    "/readyz",
	}

	srv := &Server{
		cfg:            cfg,
		store:          store.New(db, logger),
		hub:            NewHub(logger, cfg.AllowedOrigins),
		logger:         logger,
		jwtSecret:      fx.jwtSecret,
		allowedOrigins: allowedOriginsMap(cfg.AllowedOrigins),
		pro:            pro.Load(db, logger),
		loginLimiter:   newLoginLimiter(loginWindow, loginPerIP, loginPerEmail),
	}
	fx.router = srv.routes()

	return fx
}

func insertScopedPipeline(t *testing.T, db *sqlx.DB, name string, applicationID int) int {
	t.Helper()

	res, err := db.Exec(
		`INSERT INTO pipeline (name, status, created_at, is_completed, application_id) VALUES (?, ?, ?, 0, ?)`,
		name, types.PipelineStatusRunning, time.Now().UTC(), applicationID,
	)
	if err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("pipeline id: %v", err)
	}
	return int(id)
}

func insertScopedStage(t *testing.T, db *sqlx.DB, pipelineID int) int {
	t.Helper()

	res, err := db.Exec(
		`INSERT INTO stage (name, status, pipeline_id, created_at) VALUES (?, ?, ?, ?)`,
		"stage-1", types.StageStatusFailed, pipelineID, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("stage id: %v", err)
	}
	return int(id)
}
