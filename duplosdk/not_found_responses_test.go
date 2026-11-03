package duplosdk

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"
)

// kvMissingBody is what the backend answers (HTTP 400) for a Key Vault secret
// that no longer exists — a KeyNotFoundException the controller maps to 400,
// not 404 (F-11). The quotes arrive JSON-escaped.
const kvMissingBody = `{"Message":"Secret 'qa-secret' was not found in Key Vault 'tf-kv'."}`

// kvValidationBody is a genuine validation failure from the same API.
const kvValidationBody = `{"Message":"Value cannot be null. (Parameter 'Version')"}`

type notFoundRuleAPI struct {
	mu     sync.Mutex
	status int
	body   string
	calls  map[string]int
}

func (a *notFoundRuleAPI) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls[r.Method]++
	w.WriteHeader(a.status)
	_, _ = w.Write([]byte(a.body))
}

func newNotFoundRuleResource(t *testing.T, status int, body string, rules []NotFoundResponse) (*RESTResource[map[string]any], *notFoundRuleAPI) {
	t.Helper()
	api := &notFoundRuleAPI{status: status, body: body, calls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ep := Endpoint{
		UriBase:           "/v1/kv/{key_vault_id}/secrets",
		Deprovision:       Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"},
		NotFoundResponses: rules,
	}
	return NewRESTResource[map[string]any](c, ep, map[string]string{"key_vault_id": "kv1"}, nil), api
}

var kvRule = []NotFoundResponse{{Status: 400, Message: regexp.MustCompile(`^Secret '[^']+' was not found in Key Vault '[^']+'`)}}

func TestNotFoundResponses_ReadAndDeleteTreatMatchAsGone(t *testing.T) {
	r, _ := newNotFoundRuleResource(t, http.StatusBadRequest, kvMissingBody, kvRule)

	if _, err := r.Get("qa-secret"); err == nil || !err.IsNotFound() {
		t.Errorf("Get: want a not-found error for the declared 400, got %v", err)
	}
	if err := r.Delete("qa-secret"); err != nil {
		t.Errorf("Delete: the declared not-found 400 must count as already gone, got %v", err)
	}
	if err := r.Deprovision("qa-secret"); err != nil {
		t.Errorf("Deprovision: the declared not-found 400 must count as already gone, got %v", err)
	}
	if _, gone, err := r.CurrentStatus("qa-secret"); err != nil || !gone {
		t.Errorf("CurrentStatus: want gone=true, got gone=%v err=%v", gone, err)
	}
}

func TestNotFoundResponses_ValidationErrorStillFails(t *testing.T) {
	r, _ := newNotFoundRuleResource(t, http.StatusBadRequest, kvValidationBody, kvRule)

	if _, err := r.Get("qa-secret"); err == nil || err.IsNotFound() || err.Status() != 400 {
		t.Errorf("Get: a validation 400 must stay a 400 error, got %v", err)
	}
	if err := r.Delete("qa-secret"); err == nil || err.Status() != 400 {
		t.Errorf("Delete: a validation 400 must still fail, got %v", err)
	}
}

func TestNotFoundResponses_StatusMustMatch(t *testing.T) {
	// The message matches but the status does not: no mapping.
	r, _ := newNotFoundRuleResource(t, http.StatusInternalServerError, kvMissingBody, kvRule)
	if _, err := r.Get("qa-secret"); err == nil || err.IsNotFound() {
		t.Errorf("Get: a 500 must not be mapped by a 400 rule, got %v", err)
	}
}

func TestNotFoundResponses_NoRuleNoMapping(t *testing.T) {
	// Every other resource keeps the old contract: only 404 is not-found.
	r, _ := newNotFoundRuleResource(t, http.StatusBadRequest, kvMissingBody, nil)
	if _, err := r.Get("qa-secret"); err == nil || err.IsNotFound() {
		t.Errorf("Get without a rule: want the 400 error unchanged, got %v", err)
	}
	if err := r.Delete("qa-secret"); err == nil {
		t.Error("Delete without a rule: want the 400 error unchanged")
	}
}

func TestNotFoundResponses_WritesAreNotMapped(t *testing.T) {
	// Only read/delete/deprovision interpret the rule; a create or update that
	// hits it must surface the error rather than pretend nothing happened.
	r, _ := newNotFoundRuleResource(t, http.StatusBadRequest, kvMissingBody, kvRule)
	if _, err := r.Update("qa-secret", &map[string]any{}); err == nil || err.IsNotFound() {
		t.Errorf("Update: want the 400 error unchanged, got %v", err)
	}
	if _, err := r.Create(&map[string]any{}); err == nil || err.IsNotFound() {
		t.Errorf("Create: want the 400 error unchanged, got %v", err)
	}
}
