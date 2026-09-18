//go:build integration

package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPostgresAuditUntrustedTextCannotDiscardRecord(t *testing.T) {
	st := openPostgres(t)
	ctx := context.Background()
	action := "it.audit.text." + fmt.Sprint(time.Now().UnixNano())
	t.Cleanup(func() { _, _ = st.db.Exec(`DELETE FROM audit_log WHERE action=$1`, action) })
	for _, target := range []string{strings.Repeat("x", 400), strings.Repeat("界", 255), "bad\x00value\xff"} {
		if err := st.RecordAudit(ctx, AuditEntry{Action: action, Target: target, Email: strings.Repeat("a", 300), Outcome: "failed", RemoteAddr: strings.Repeat("x", 100)}); err != nil {
			t.Fatalf("untrusted audit metadata discarded the record: %v", err)
		}
	}
	page, err := st.ListAuditFiltered(ctx, AuditFilter{Action: action})
	if err != nil || page.TotalCount != 3 {
		t.Fatalf("audit record count = %+v, err=%v", page, err)
	}
	for _, entry := range page.Items {
		if entry.Target == nil || !utf8.ValidString(*entry.Target) || utf8.RuneCountInString(*entry.Target) > 255 || strings.ContainsRune(*entry.Target, 0) {
			t.Fatalf("stored target is not valid bounded text: %+v", entry)
		}
		if entry.Email == nil || utf8.RuneCountInString(*entry.Email) != 255 || entry.RemoteAddr == nil || utf8.RuneCountInString(*entry.RemoteAddr) != 64 {
			t.Fatalf("stored metadata does not fit schema: %+v", entry)
		}
	}
	if *page.Items[1].Target != strings.Repeat("界", 255) {
		t.Fatal("varchar limits must count characters without truncating valid Unicode")
	}
}
