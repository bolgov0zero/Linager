package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestDashboardScheme: the layout of the scheme view is saved per user, comes back with the dashboard, is checked
// (positions on the plane within a bound, a limited number of nodes), can be reset, and one user's layout is not
// another's.
func TestDashboardScheme(t *testing.T) {
	base, _ := startAccountsServer(t)
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	type dash struct {
		Scheme *struct {
			Panel *struct{ X, Y float64 }           `json:"panel"`
			Nodes map[string]struct{ X, Y float64 } `json:"nodes"`
		} `json:"scheme"`
	}
	var d dash
	if code := admin.do("GET", "/api/dashboard", nil, &d); code != 200 || d.Scheme != nil {
		t.Fatalf("a new dashboard has no layout: %d %+v", code, d.Scheme)
	}

	layout := map[string]any{
		"panel": map[string]float64{"x": 0, "y": 0},
		"nodes": map[string]any{"11111111-1111-1111-1111-111111111111": map[string]float64{"x": -420, "y": 190}},
	}
	if code := admin.do("PUT", "/api/dashboard/scheme", layout, nil); code != 200 {
		t.Fatalf("save: %d", code)
	}
	d = dash{}
	if code := admin.do("GET", "/api/dashboard", nil, &d); code != 200 || d.Scheme == nil || d.Scheme.Panel == nil ||
		d.Scheme.Nodes["11111111-1111-1111-1111-111111111111"].X != -420 {
		t.Fatalf("the layout comes back with the dashboard: %d %+v", code, d.Scheme)
	}

	// a position beyond the bound, an empty or long id, or too many nodes is refused
	bad := []map[string]any{
		{"nodes": map[string]any{"a": map[string]float64{"x": 50001, "y": 0}}},
		{"nodes": map[string]any{"a": map[string]float64{"x": 0, "y": -50001}}},
		{"panel": map[string]float64{"x": 1e9, "y": 0}, "nodes": map[string]any{}},
		{"nodes": map[string]any{"": map[string]float64{"x": 0, "y": 0}}},
		{"nodes": map[string]any{strings.Repeat("a", 37): map[string]float64{"x": 0, "y": 0}}},
	}
	for i, b := range bad {
		if code := admin.do("PUT", "/api/dashboard/scheme", b, nil); code != http.StatusUnprocessableEntity {
			t.Fatalf("bad layout %d must be refused: %d", i, code)
		}
	}
	// 500 nodes with ids of the real length are the most that is kept; 501 are refused
	many := map[string]any{}
	for i := 0; i < 500; i++ {
		many[fmt.Sprintf("%036d", i)] = map[string]float64{"x": -1234.5, "y": 6543.25}
	}
	if code := admin.do("PUT", "/api/dashboard/scheme", map[string]any{"nodes": many}, nil); code != 200 {
		t.Fatalf("500 nodes must be accepted: %d", code)
	}
	many[fmt.Sprintf("%036d", 500)] = map[string]float64{"x": 0, "y": 0}
	if code := admin.do("PUT", "/api/dashboard/scheme", map[string]any{"nodes": many}, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("501 nodes must be refused: %d", code)
	}
	if code := admin.do("PUT", "/api/dashboard/scheme", layout, nil); code != 200 { // back to the small layout
		t.Fatalf("save again: %d", code)
	}

	// another user has a layout of their own
	if code := admin.do("POST", "/api/users", map[string]any{"login": "auditor", "password": "auditor-long-password", "role": "auditor", "servers": []string{}, "modules": []string{}}, nil); code != 200 {
		t.Fatalf("create auditor: %d", code)
	}
	aud := mustLogin(t, base, "auditor", "auditor-long-password")
	d = dash{}
	if code := aud.do("GET", "/api/dashboard", nil, &d); code != 200 || d.Scheme != nil {
		t.Fatalf("the auditor does not see the admin's layout: %d %+v", code, d.Scheme)
	}
	if code := aud.do("PUT", "/api/dashboard/scheme", map[string]any{"nodes": map[string]any{}}, nil); code != 200 {
		t.Fatalf("the auditor saves their own layout: %d", code)
	}

	// a reset returns the automatic layout, and only for the one who asked
	if code := admin.do("DELETE", "/api/dashboard/scheme", nil, nil); code != 200 {
		t.Fatalf("reset: %d", code)
	}
	d = dash{}
	if code := admin.do("GET", "/api/dashboard", nil, &d); code != 200 || d.Scheme != nil {
		t.Fatalf("after a reset there is no layout: %d %+v", code, d.Scheme)
	}
	d = dash{}
	if code := aud.do("GET", "/api/dashboard", nil, &d); code != 200 || d.Scheme == nil {
		t.Fatalf("the auditor's layout stays: %d %+v", code, d.Scheme)
	}
}
