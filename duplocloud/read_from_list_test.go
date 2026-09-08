package duplocloud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	got, err := readCollectionElementAt(collectionAPI(t, srv), "", "id", "bbb")
	if err != nil {
		t.Fatalf("readCollectionElementAt: %v", err)
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

	got, err := readCollectionElementAt(collectionAPI(t, srv), "", "id", "does-not-exist")
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

	got, err := readCollectionElementAt(collectionAPI(t, srv), "", "id", "aaa")
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

	got, err := readCollectionElementAt(collectionAPI(t, srv), "", "meta.id", "x2")
	if err != nil {
		t.Fatalf("readCollectionElementAt: %v", err)
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

// newEnvelopeCollectionServer mirrors the security-group-ingress routes, whose
// collection response wraps its elements in an object instead of being a bare
// array: {"ownSecurityGroupId":"sg-…","rules":[…]}.
func newEnvelopeCollectionServer(t *testing.T, listKey string, items []map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/things" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"ownSecurityGroupId": "sg-0fb591b8a516a100f",
				listKey:              items,
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An enveloped collection must decode through the listPath. Reading it as a
// bare array is the reported bug: "cannot unmarshal object into Go struct field
// apiResponse[[]map[string]interface {}].data of type []map[string]interface {}".
func TestReadFromList_EnvelopedCollectionSelectsElement(t *testing.T) {
	srv := newEnvelopeCollectionServer(t, "rules", []map[string]any{
		{"securityGroupRuleId": "sgr-111", "cidrIpv4": "10.0.0.0/8"},
		{"securityGroupRuleId": "sgr-222", "cidrIpv4": "192.168.0.0/16"},
	})

	got, err := readCollectionElementAt(collectionAPI(t, srv), "rules", "securityGroupRuleId", "sgr-222")
	if err != nil {
		t.Fatalf("readCollectionElementAt: %v", err)
	}
	if got == nil {
		t.Fatal("element sgr-222 not found in the enveloped collection")
	}
	if (*got)["cidrIpv4"] != "192.168.0.0/16" {
		t.Errorf("selected the wrong element: %v", *got)
	}
}

// The same read without the listPath is what the data source used to do, and it
// fails to decode — this pins the failure so the fix cannot silently regress.
func TestReadFromList_EnvelopedCollectionWithoutListPathFails(t *testing.T) {
	srv := newEnvelopeCollectionServer(t, "rules", []map[string]any{
		{"securityGroupRuleId": "sgr-111"},
	})

	_, err := readCollectionElementAt(collectionAPI(t, srv), "", "securityGroupRuleId", "sgr-111")
	if err == nil {
		t.Fatal("expected an unmarshal error when an enveloped collection is read as a bare array")
	}
}

// An element absent from an enveloped collection is "gone", not an error — the
// data source turns nil into its own not-found diagnostic.
func TestReadFromList_EnvelopedCollectionMissingElement(t *testing.T) {
	srv := newEnvelopeCollectionServer(t, "rules", []map[string]any{
		{"securityGroupRuleId": "sgr-111"},
	})

	got, err := readCollectionElementAt(collectionAPI(t, srv), "rules", "securityGroupRuleId", "sgr-999")
	if err != nil {
		t.Fatalf("readCollectionElementAt: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for a missing element, got %v", *got)
	}
}

// The resource and the data source must resolve a collection read identically.
// They now share specCollectionElement, so this drives the same function both
// call and proves it honors the spec's readListPath — the divergence that broke
// the security-group-rule data sources while their resources worked.
func TestSpecCollectionElement_HonorsSpecReadListPath(t *testing.T) {
	srv := newEnvelopeCollectionServer(t, "rules", []map[string]any{
		{"securityGroupRuleId": "sgr-abc", "ipProtocol": "tcp"},
	})
	spec := &ResourceSpec{
		Name:     "resource_group_security_group_rule",
		IDPath:   "securityGroupRuleId",
		Endpoint: EndpointSpec{ReadFromList: true, ReadListPath: "rules"},
	}

	got, err := specCollectionElement(spec, collectionAPI(t, srv), "sgr-abc")
	if err != nil {
		t.Fatalf("specCollectionElement: %v", err)
	}
	if got == nil || (*got)["ipProtocol"] != "tcp" {
		t.Fatalf("expected the tcp rule, got %v", got)
	}
}

// knownCollectionShapes records, per collection route, whether the API wraps its
// elements in an object ({"ownSecurityGroupId":…,"rules":[…]}) or answers with a
// bare array. Nothing in a spec file can express this — only the live API knows —
// so the shapes are recorded here from observed responses, and the test below
// fails on any readFromList route missing from the table.
//
// Adding a readFromList spec therefore forces a deliberate answer to "what shape
// does this route return?", which is the question that was got wrong for the
// security-group-rule data sources.
var knownCollectionShapes = map[string]struct {
	wrapped bool
	note    string
}{
	"awsSecurityGroupIngresses": {true, `{"ownSecurityGroupId":…,"rules":[…]} — elements under "rules"`},
	"kms-keys":                  {false, "a bare array of key registrations"},
}

// routeCollection is the last path segment of a uriBase — the collection name the
// shapes above are keyed by.
func routeCollection(uriBase string) string {
	segs := strings.Split(strings.TrimSuffix(uriBase, "/"), "/")
	return segs[len(segs)-1]
}

// Each shipped readFromList spec must declare readListPath exactly when its route
// wraps its elements: set on a wrapped route (or the response cannot be decoded at
// all, which is the reported bug), absent on a bare array (or the decode looks for
// a key that is not there).
func TestShippedReadFromListSpecsMatchKnownRouteShape(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	seen := 0
	for _, spec := range specs {
		if !spec.Endpoint.ReadFromList {
			continue
		}
		seen++
		coll := routeCollection(spec.Endpoint.UriBase)
		shape, known := knownCollectionShapes[coll]
		if !known {
			t.Errorf("%s: readFromList on route %q, whose response shape is not recorded — "+
				"observe the collection response and add %q to knownCollectionShapes",
				spec.Name, coll, coll)
			continue
		}
		switch {
		case shape.wrapped && spec.Endpoint.ReadListPath == "":
			t.Errorf("%s: route %q returns %s, so readListPath is required",
				spec.Name, coll, shape.note)
		case !shape.wrapped && spec.Endpoint.ReadListPath != "":
			t.Errorf("%s: route %q returns %s, so readListPath must not be set (got %q)",
				spec.Name, coll, shape.note, spec.Endpoint.ReadListPath)
		}
	}
	if seen == 0 {
		t.Fatal("no readFromList specs found — the walk is not covering anything")
	}
}

// The table itself must stay honest: a shape recorded for a route no spec uses is
// dead weight that outlives the route it described.
func TestKnownCollectionShapesHasNoUnusedEntries(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	used := map[string]bool{}
	for _, spec := range specs {
		if spec.Endpoint.ReadFromList {
			used[routeCollection(spec.Endpoint.UriBase)] = true
		}
	}
	for coll := range knownCollectionShapes {
		if !used[coll] {
			t.Errorf("knownCollectionShapes records %q, which no readFromList spec uses any more", coll)
		}
	}
}
