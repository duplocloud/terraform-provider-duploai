package duplocloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// readFromList exists because a sub-collection like .../kms-keys serves GET on
// the collection and DELETE on /{id}, but has no GET on /{id} — the conventional
// read gets 405 Method Not Allowed, so every plan after the first fails. These
// tests pin the selection and the "gone" signal, and that the request goes to the
// collection rather than to /{id}.
func newCollectionServer(t *testing.T, items []map[string]any) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.Method+" "+req.URL.Path)
		if req.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Mirror the real API: only the collection path answers GET.
		if req.URL.Path != "/things" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": items})
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func collectionAPI(t *testing.T, srv *httptest.Server) *duplosdk.RESTResource[map[string]any] {
	t.Helper()
	c, err := duplosdk.NewClient(srv.URL, "token", false, 30*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return duplosdk.NewRESTResource[map[string]any](c, duplosdk.Endpoint{UriBase: "/things"}, map[string]string{}, nil)
}

func TestReadFromList_SelectsMatchingElement(t *testing.T) {
	srv, paths := newCollectionServer(t, []map[string]any{
		{"id": "aaa", "keyName": "first"},
		{"id": "bbb", "keyName": "second"},
	})

	got, err := readCollectionElement(collectionAPI(t, srv), "id", "bbb")
	if err != nil {
		t.Fatalf("readCollectionElement: %v", err)
	}
	if got == nil {
		t.Fatal("element bbb not found")
	}
	if (*got)["keyName"] != "second" {
		t.Errorf("selected %v, want the element with keyName=second", *got)
	}
	// The whole point: the request must hit the collection, never /{id}, which
	// is the path that answers 405.
	if len(*paths) != 1 || (*paths)[0] != "GET /things" {
		t.Errorf("requests = %v, want exactly [GET /things]", *paths)
	}
}

func TestReadFromList_MissingElementIsGoneNotError(t *testing.T) {
	srv, _ := newCollectionServer(t, []map[string]any{{"id": "aaa"}})

	got, err := readCollectionElement(collectionAPI(t, srv), "id", "does-not-exist")
	if err != nil {
		t.Fatalf("a collection that simply lacks the element must not error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil so the caller drops the resource from state", *got)
	}
}

// An empty collection is a legitimate answer, not a decode failure — the caller
// must treat it as "gone" rather than surfacing an error to the user.
func TestReadFromList_EmptyCollection(t *testing.T) {
	srv, _ := newCollectionServer(t, []map[string]any{})

	got, err := readCollectionElement(collectionAPI(t, srv), "id", "aaa")
	if err != nil {
		t.Fatalf("empty collection errored: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", *got)
	}
}

// idPath may be nested (the flag is generic, even though today's users key on a
// top-level "id").
func TestReadFromList_NestedIDPath(t *testing.T) {
	srv, _ := newCollectionServer(t, []map[string]any{
		{"meta": map[string]any{"id": "x1"}, "name": "one"},
		{"meta": map[string]any{"id": "x2"}, "name": "two"},
	})

	got, err := readCollectionElement(collectionAPI(t, srv), "meta.id", "x2")
	if err != nil {
		t.Fatalf("readCollectionElement: %v", err)
	}
	if got == nil || (*got)["name"] != "two" {
		t.Errorf("got %v, want the element named two", got)
	}
}

// Both KMS registries must carry the flag: without it their refresh 405s and
// every plan after the first fails.
func TestReadFromList_KmsSpecsOptIn(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	want := map[string]bool{"plan_kms_key": false, "resource_group_kms_key": false}
	for _, s := range specs {
		if _, ok := want[s.Name]; ok {
			want[s.Name] = s.Endpoint.ReadFromList
		}
	}
	for name, on := range want {
		if !on {
			t.Errorf("%s must set endpoint.readFromList — its per-element GET returns 405", name)
		}
	}
}
