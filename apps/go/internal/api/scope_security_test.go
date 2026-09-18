package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestApplicationScope_ApplicationUpdateRequiresMembership(t *testing.T) {
	fx := setupScopeTest(t)
	response := fx.do(t, scopedUserID, http.MethodPost, "/applications", map[string]any{"id": otherApplicationID, "name": "changed"})
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign application update = %d, want 404: %s", response.Code, response.Body.String())
	}
	var name string
	if err := fx.db.Get(&name, `SELECT name FROM application WHERE id = ?`, otherApplicationID); err != nil || name != "other" {
		t.Fatalf("foreign application changed: name=%q err=%v", name, err)
	}
	var audit struct{ Action, Target, Outcome string }
	if err := fx.db.Get(&audit, `SELECT action, target, outcome FROM audit_log ORDER BY id DESC LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if audit.Action != "application.update" || audit.Target != "application:2" || audit.Outcome != "failed" {
		t.Fatalf("denied foreign update audit = %+v", audit)
	}
	response = fx.do(t, scopedUserID, http.MethodPost, "/applications", map[string]any{"id": ownApplicationID, "name": "changed"})
	if response.Code != http.StatusOK {
		t.Fatalf("own application update = %d: %s", response.Code, response.Body.String())
	}
	if err := fx.db.Get(&name, `SELECT name FROM application WHERE id = ?`, ownApplicationID); err != nil || name != "changed" {
		t.Fatalf("own application not updated: name=%q err=%v", name, err)
	}
	response = fx.do(t, scopedUserID, http.MethodPost, "/applications", map[string]any{"name": "new"})
	if response.Code != http.StatusOK {
		t.Fatalf("application creation = %d: %s", response.Code, response.Body.String())
	}
}

func TestApplicationScope_KeywordsOnlyRevealLinkedApplications(t *testing.T) {
	fx := setupScopeTest(t)
	_, err := fx.db.Exec(`
		CREATE TABLE log (id INTEGER PRIMARY KEY, application_id INTEGER NOT NULL);
		CREATE TABLE log_keyword (log_id INTEGER, keyword_id INTEGER);
		INSERT INTO keyword (id, key, value) VALUES (1,'own-pipeline','value'),(2,'foreign-pipeline','value'),(3,'orphan','value'),(4,'own-log','value'),(5,'foreign-log','value');
		INSERT INTO pipeline_keyword (pipeline_id, keyword_id) VALUES (?,1),(?,2);
		INSERT INTO log (id, application_id) VALUES (1,1),(2,2);
		INSERT INTO log_keyword (log_id, keyword_id) VALUES (1,4),(2,5);
	`, fx.ownPipelineID, fx.otherPipelineID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		userID int
		path   string
		want   []string
	}{
		{"member", scopedUserID, "/keywords", []string{"own-log", "own-pipeline"}},
		{"member search", scopedUserID, "/keywords?keySearch=PIPE", []string{"own-pipeline"}},
		{"foreign search", scopedUserID, "/keywords?keySearch=foreign", []string{}},
		{"no memberships", strangerUserID, "/keywords", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := fx.do(t, tc.userID, http.MethodGet, tc.path, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("keywords = %d: %s", response.Code, response.Body.String())
			}
			var keywords []string
			if err := json.Unmarshal(response.Body.Bytes(), &keywords); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(keywords, tc.want) {
				t.Fatalf("keywords = %v, want %v", keywords, tc.want)
			}
		})
	}
}
