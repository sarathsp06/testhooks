package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPI_GetLimits(t *testing.T) {
	api := newTestAPI(newMockStore())
	api.SetLimits(Limits{MaxRequestsPerEndpoint: 500, MaxEndpointStorageBytes: 10 << 20, MaxBodySize: 512 << 10})
	mux := newMux(api)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/limits", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["max_requests_per_endpoint"] != float64(500) {
		t.Errorf("max_requests_per_endpoint = %v, want 500", got["max_requests_per_endpoint"])
	}
	if _, ok := got["upgrade_note"]; ok {
		t.Errorf("upgrade_note should be omitted when empty, got %v", got["upgrade_note"])
	}

	api.SetLimits(Limits{UpgradeNote: "A paid plan would keep more."})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/limits", nil))
	if !strings.Contains(w.Body.String(), `"upgrade_note":"A paid plan would keep more."`) {
		t.Errorf("body = %s, want upgrade_note", w.Body.String())
	}
}

func TestAPI_RecordUpgradeInterest_CountsPeopleNotClicks(t *testing.T) {
	mux := newMux(newTestAPI(newMockStore()))

	click := func(clientID string) int {
		req := httptest.NewRequest("POST", "/api/upgrade-interest", strings.NewReader(`{"endpoint_id":"ep-1"}`))
		if clientID != "" {
			req.Header.Set("X-Client-ID", clientID)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var got struct{ Count int }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Count
	}

	if n := click("browser-a"); n != 1 {
		t.Errorf("first click count = %d, want 1", n)
	}
	if n := click("browser-a"); n != 1 {
		t.Errorf("same browser again count = %d, want 1", n)
	}
	if n := click("browser-b"); n != 2 {
		t.Errorf("second browser count = %d, want 2", n)
	}
	// No client id (curl): each call counts once.
	if n := click(""); n != 3 {
		t.Errorf("anonymous click count = %d, want 3", n)
	}
}
