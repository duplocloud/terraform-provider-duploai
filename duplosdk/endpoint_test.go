package duplosdk

import (
	"net/http"
	"reflect"
	"testing"
)

func TestEndpointHasUpdate(t *testing.T) {
	withUpdate := Endpoint{Update: Operation{Verb: http.MethodPut, Path: "/{id}"}}
	if !withUpdate.HasUpdate() {
		t.Error("endpoint with an Update operation should report HasUpdate() = true")
	}
	immutable := Endpoint{Create: Operation{Verb: http.MethodPost}, Delete: Operation{Verb: http.MethodDelete, Path: "/{id}"}}
	if immutable.HasUpdate() {
		t.Error("endpoint without an Update operation should report HasUpdate() = false")
	}
}

func TestEndpointPathParams(t *testing.T) {
	e := Endpoint{UriBase: "/v3/subscriptions/{tenant_id}/clusters/{cluster_id}/nodepools"}
	if got := e.PathParams(); !reflect.DeepEqual(got, []string{"tenant_id", "cluster_id"}) {
		t.Errorf("PathParams() = %v", got)
	}
}

func TestEndpointDefaultPaths(t *testing.T) {
	scope := map[string]string{"tenant_id": "t-1", "cluster_id": "c-9"}
	e := Endpoint{UriBase: "/v3/subscriptions/{tenant_id}/clusters/{cluster_id}/nodepools"}

	// Create posts to UriBase; read/update/delete default to UriBase + "/{id}".
	if got := e.createPath(scope); got != "/v3/subscriptions/t-1/clusters/c-9/nodepools" {
		t.Errorf("createPath = %q", got)
	}
	want := "/v3/subscriptions/t-1/clusters/c-9/nodepools/np-7"
	for _, got := range []string{e.readPath(scope, "np-7"), e.updatePath(scope, "np-7"), e.deletePath(scope, "np-7")} {
		if got != want {
			t.Errorf("item path = %q, want %q", got, want)
		}
	}
}

func TestEndpointCustomReadPath(t *testing.T) {
	// Read URI differs; the rest stay default.
	e := Endpoint{
		UriBase: "/x/{tenant_id}/things",
		Read:    Operation{Path: "/get/{id}"},
	}
	scope := map[string]string{"tenant_id": "t-1"}
	if got := e.readPath(scope, "o-2"); got != "/x/t-1/things/get/o-2" {
		t.Errorf("readPath = %q", got)
	}
	if got := e.updatePath(scope, "o-2"); got != "/x/t-1/things/o-2" {
		t.Errorf("updatePath should stay default = %q", got)
	}
}

func TestEndpointPerOperation(t *testing.T) {
	// Action-style API: every operation has its own path and verb.
	e := Endpoint{
		UriBase: "/svc/{tenant_id}",
		Create:  Operation{Verb: http.MethodPost, Path: "/CreateFoo"},
		Read:    Operation{Verb: http.MethodGet, Path: "/GetFoo/{id}"},
		Update:  Operation{Verb: http.MethodPost, Path: "/UpdateFoo/{id}"},
		Delete:  Operation{Verb: http.MethodPost, Path: "/DeleteFoo/{id}"},
	}
	scope := map[string]string{"tenant_id": "t-9"}
	if got := e.createPath(scope); got != "/svc/t-9/CreateFoo" {
		t.Errorf("createPath = %q", got)
	}
	if got := e.readPath(scope, "f-1"); got != "/svc/t-9/GetFoo/f-1" {
		t.Errorf("readPath = %q", got)
	}
	if got := e.updatePath(scope, "f-1"); got != "/svc/t-9/UpdateFoo/f-1" {
		t.Errorf("updatePath = %q", got)
	}
	if e.updateVerb() != http.MethodPost || e.deleteVerb() != http.MethodPost {
		t.Error("verb overrides not applied")
	}
	// Scope still parses from UriBase.
	if got := e.PathParams(); !reflect.DeepEqual(got, []string{"tenant_id"}) {
		t.Errorf("PathParams = %v", got)
	}
}

func TestEndpointVerbs(t *testing.T) {
	def := Endpoint{}
	if def.createVerb() != http.MethodPost || def.readVerb() != http.MethodGet ||
		def.updateVerb() != http.MethodPut || def.deleteVerb() != http.MethodDelete {
		t.Error("default verbs incorrect")
	}
	custom := Endpoint{Update: Operation{Verb: http.MethodPost}, Delete: Operation{Verb: http.MethodPost}}
	if custom.updateVerb() != http.MethodPost || custom.deleteVerb() != http.MethodPost {
		t.Error("verb override not applied")
	}
	if custom.createVerb() != http.MethodPost || custom.readVerb() != http.MethodGet {
		t.Error("unset verbs should keep defaults")
	}
}
