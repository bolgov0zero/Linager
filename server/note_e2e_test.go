package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestServerNote: the note of a server is saved by an administrator, comes back with the server, is limited in
// length, is written to the audit without its text, and cannot be changed by the other roles.
func TestServerNote(t *testing.T) {
	base, db := startAccountsServer(t)
	ctx := context.Background()
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO servers (name,hostname,address,agent_pubkey,os,agent_version,protocol)
		VALUES ('web','web','10.0.0.1','\x00','Debian','0.1.0',1) RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	var det struct {
		Note      string `json:"note"`
		UpdatedAt int64  `json:"note_updated_at"`
		UpdatedBy string `json:"note_updated_by"`
	}
	if code := admin.do("GET", "/api/servers/"+id, nil, &det); code != 200 || det.Note != "" || det.UpdatedAt != 0 {
		t.Fatalf("a new server has no note: %d %+v", code, det)
	}
	text := "# Owner\r\nTeam **infra**, backups on `nas-1`.\r\n- one\r\n- two"
	var saved det2
	if code := admin.do("PUT", "/api/servers/"+id+"/note", map[string]string{"note": text}, &saved); code != 200 || saved.Note != strings.ReplaceAll(text, "\r\n", "\n") || saved.UpdatedBy != "admin" || saved.UpdatedAt == 0 {
		t.Fatalf("save: %d %+v", code, saved)
	}
	if code := admin.do("GET", "/api/servers/"+id, nil, &det); code != 200 || det.Note != saved.Note || det.UpdatedBy != "admin" {
		t.Fatalf("the note comes back with the server: %d %+v", code, det)
	}
	// an empty note clears it
	if code := admin.do("PUT", "/api/servers/"+id+"/note", map[string]string{"note": ""}, &saved); code != 200 || saved.Note != "" {
		t.Fatalf("clear: %d %+v", code, saved)
	}
	// the length limit counts characters, not bytes
	if code := admin.do("PUT", "/api/servers/"+id+"/note", map[string]string{"note": strings.Repeat("я", 20000)}, nil); code != 200 {
		t.Fatalf("20000 characters must be accepted: %d", code)
	}
	if code := admin.do("PUT", "/api/servers/"+id+"/note", map[string]string{"note": strings.Repeat("я", 20001)}, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("20001 characters must be refused: %d", code)
	}
	if code := admin.do("PUT", "/api/servers/00000000-0000-0000-0000-000000000000/note", map[string]string{"note": "x"}, nil); code != http.StatusNotFound {
		t.Fatalf("an unknown server: %d", code)
	}

	// the journal records that the note changed, not what it says
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='server.note' AND target_id=$1 AND details::text NOT LIKE '%infra%'`, id).Scan(&n); err != nil || n != 3 {
		t.Fatalf("audit entries: %d %v", n, err)
	}

	// an operator and an auditor read the note with the server but cannot change it
	for _, role := range []string{"operator", "auditor"} {
		if code := admin.do("POST", "/api/users", map[string]any{"login": role, "password": role + "-long-password", "role": role, "servers": []string{id}, "modules": []string{}}, nil); code != 200 {
			t.Fatalf("create %s: %d", role, code)
		}
		c := mustLogin(t, base, role, role+"-long-password")
		if code := c.do("PUT", "/api/servers/"+id+"/note", map[string]string{"note": "mine"}, nil); code != http.StatusForbidden {
			t.Fatalf("%s must not change the note: %d", role, code)
		}
		if code := c.do("GET", "/api/servers/"+id, nil, &det); code != 200 {
			t.Fatalf("%s must read the server: %d", role, code)
		}
	}
}

type det2 struct {
	Note      string `json:"note"`
	UpdatedAt int64  `json:"note_updated_at"`
	UpdatedBy string `json:"note_updated_by"`
}
