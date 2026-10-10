package api

// Round 87 filters: F5693 update skipped the create-time validation (caps and
// empty values), F5694 reorder was stored but never honored by the listing
// (entries came back in key order), F5695 a partial update reset matchAll.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const f5693User = "sender@sendtest.invalid"

func f5693Call(srv *Server, h http.HandlerFunc, method, url, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), "user", f5693User))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func f5693Create(t *testing.T, srv *Server, name string) EmailFilter {
	t.Helper()
	rec := f5693Call(srv, srv.handleFilters, http.MethodPost, "/api/v1/filters",
		`{"name":"`+name+`","matchAll":true,"conditions":[{"field":"from","operator":"contains","value":"x"}],"actions":[{"type":"markRead"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s = %d %s", name, rec.Code, rec.Body.String())
	}
	var f EmailFilter
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func f5693List(t *testing.T, srv *Server) []string {
	t.Helper()
	rec := f5693Call(srv, srv.handleFilters, http.MethodGet, "/api/v1/filters", "")
	var out struct {
		Filters []EmailFilter `json:"filters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range out.Filters {
		names = append(names, f.Name)
	}
	return names
}

func TestAudit5693Control(t *testing.T) {
	srv, _ := webmailSendServer(t)
	f := f5693Create(t, srv, "one")
	rec := f5693Call(srv, srv.handleFilter, http.MethodPut, "/api/v1/filters/"+f.ID,
		`{"conditions":[{"field":"header","headerName":"X-A","operator":"contains","value":"v"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid update accepted? got %d", rec.Code)
	}
	// Create keeps rejecting an empty condition value.
	rec = f5693Call(srv, srv.handleFilters, http.MethodPost, "/api/v1/filters",
		`{"name":"n","conditions":[{"field":"from","value":""}],"actions":[{"type":"markRead"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create empty value = %d", rec.Code)
	}
}

func TestAudit5693Failure(t *testing.T) {
	srv, _ := webmailSendServer(t)
	f := f5693Create(t, srv, "one")
	bodies := map[string]string{
		"empty value":      `{"conditions":[{"field":"from","operator":"contains","value":""}]}`,
		"header no name":   `{"conditions":[{"field":"header","operator":"contains","value":"v"}]}`,
		"long value":       `{"conditions":[{"field":"from","value":"` + strings.Repeat("a", 1001) + `"}]}`,
		"long name":        `{"name":"` + strings.Repeat("n", 256) + `"}`,
		"too many actions": `{"actions":[` + strings.TrimSuffix(strings.Repeat(`{"type":"markRead"},`, 21), ",") + `]}`,
	}
	for name, b := range bodies {
		rec := f5693Call(srv, srv.handleFilter, http.MethodPut, "/api/v1/filters/"+f.ID, b)
		t.Logf("EXPECTED: 400 ACTUAL: %s -> %d", name, rec.Code)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("DEFECT F5693: update with %s = %d", name, rec.Code)
		}
	}
	// Rejected updates must not have changed the stored filter.
	rec := f5693Call(srv, srv.handleFilter, http.MethodGet, "/api/v1/filters/"+f.ID, "")
	var got EmailFilter
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "one" || len(got.Conditions) != 1 || got.Conditions[0].Value != "x" || len(got.Actions) != 1 {
		t.Errorf("rejected update changed the filter (name len %d, %d conditions, %d actions)", len(got.Name), len(got.Conditions), len(got.Actions))
	}
}

func TestAudit5694Control(t *testing.T) {
	srv, _ := webmailSendServer(t)
	f5693Create(t, srv, "a")
	f5693Create(t, srv, "b")
	if got := strings.Join(f5693List(t, srv), ","); got != "a,b" && got != "b,a" {
		t.Fatalf("list = %s", got)
	}
}

func TestAudit5694Failure(t *testing.T) {
	srv, _ := webmailSendServer(t)
	var ids []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		ids = append(ids, f5693Create(t, srv, n).ID)
	}
	want := []string{"f", "e", "d", "c", "b", "a"}
	rev := make([]string, len(ids))
	for i, id := range ids {
		rev[len(ids)-1-i] = id
	}
	b, _ := json.Marshal(map[string][]string{"filterIds": rev})
	if rec := f5693Call(srv, srv.handleFilterReorder, http.MethodPost, "/api/v1/filters/reorder", string(b)); rec.Code != 200 {
		t.Fatalf("reorder = %d", rec.Code)
	}
	got := f5693List(t, srv)
	t.Logf("EXPECTED: %v ACTUAL: %v", want, got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("DEFECT F5694: reorder not reflected in the filter list")
	}
}

func TestAudit5694Edges(t *testing.T) {
	srv, _ := webmailSendServer(t)
	a := f5693Create(t, srv, "a")
	b := f5693Create(t, srv, "b")
	// Unknown ids are skipped; a later reorder wins; new filters append last.
	body, _ := json.Marshal(map[string][]string{"filterIds": {"nope", b.ID, a.ID}})
	f5693Call(srv, srv.handleFilterReorder, http.MethodPost, "/api/v1/filters/reorder", string(body))
	f5693Create(t, srv, "c")
	if got := strings.Join(f5693List(t, srv), ","); got != "b,a,c" {
		t.Errorf("after reorder+create = %s, want b,a,c", got)
	}
}

func TestAudit5695Failure(t *testing.T) {
	srv, _ := webmailSendServer(t)
	f := f5693Create(t, srv, "one") // matchAll=true
	rec := f5693Call(srv, srv.handleFilter, http.MethodPut, "/api/v1/filters/"+f.ID, `{"name":"renamed"}`)
	var got EmailFilter
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	t.Logf("EXPECTED: matchAll=true ACTUAL: matchAll=%v", got.MatchAll)
	if rec.Code != 200 || !got.MatchAll {
		t.Errorf("DEFECT F5695: partial update reset matchAll")
	}
	// An explicit false still applies.
	rec = f5693Call(srv, srv.handleFilter, http.MethodPut, "/api/v1/filters/"+f.ID, `{"matchAll":false}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.MatchAll {
		t.Errorf("explicit matchAll=false ignored")
	}
}
