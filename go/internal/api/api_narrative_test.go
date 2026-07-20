package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleNarrative_EmptyDeps(t *testing.T) {
	srv := New(&Deps{})
	req := httptest.NewRequest(http.MethodGet, "/api/narrative?window=now", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var n map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	if _, ok := n["facts"]; !ok {
		t.Fatalf("expected facts in response: %v", n)
	}
	// Empty live scene still yields a calm fallback body.
	body, _ := n["body"].([]any)
	if len(body) == 0 && n["action"] == nil {
		t.Fatalf("expected some prose, got %v", n)
	}
}
