package api

// F7784: the account portal saves a filter by sending its whole object
// (id, enabled, priority included); both handlers decode strictly and
// rejected it, so no filter could be created or edited from the portal.

import (
	"encoding/json"
	"net/http"
	"testing"
)

const portalFilterBody = `"conditions":[{"field":"from","operator":"endsWith","value":"@remote.example"}],"actions":[{"type":"move","target":"Work"}]`

func TestFiltersAcceptThePortalPayload(t *testing.T) {
	s, _ := contactsServer(t, false)
	list := func() []map[string]interface{} {
		var out struct{ Filters []map[string]interface{} }
		rec := contactsDo(t, s, http.MethodGet, "/api/v1/filters", "", false)
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("list: %v %s", err, rec.Body)
		}
		return out.Filters
	}

	// A new filter, exactly as the portal's editor sends it.
	rec := contactsDo(t, s, http.MethodPost, "/api/v1/filters", `{"id":"","name":"remote","enabled":true,"matchAll":true,`+portalFilterBody+`,"priority":0}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ID       string
		Enabled  bool
		Priority int
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || !created.Enabled {
		t.Fatalf("created %+v", created)
	}

	// enabled:false at creation is honoured; a client's id/priority are not.
	rec = contactsDo(t, s, http.MethodPost, "/api/v1/filters", `{"id":"client-chosen","name":"off","enabled":false,"matchAll":true,`+portalFilterBody+`,"priority":99}`, false)
	var off struct {
		ID       string
		Enabled  bool
		Priority int
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &off)
	if rec.Code != http.StatusCreated || off.Enabled || off.ID == "client-chosen" || off.Priority != 1 {
		t.Fatalf("second create: %d %+v", rec.Code, off)
	}

	// Editing sends the whole object back, with the stored id and priority.
	rec = contactsDo(t, s, http.MethodPut, "/api/v1/filters/"+created.ID,
		`{"id":"`+created.ID+`","name":"renamed","enabled":false,"matchAll":true,`+portalFilterBody+`,"priority":5}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	for _, f := range list() {
		if f["id"] == created.ID && (f["name"] != "renamed" || f["enabled"] != false || f["priority"] != float64(0)) {
			t.Errorf("after update: %v (priority must stay with the server)", f)
		}
	}

	// A field nothing knows is still an error.
	if rec := contactsDo(t, s, http.MethodPost, "/api/v1/filters", `{"name":"x","bogus":1,"matchAll":true,`+portalFilterBody+`}`, false); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", rec.Code)
	}
}
