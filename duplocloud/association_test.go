package duplocloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// The admin_workspace_scope_mapping shape: both ids are path parameters, the
// link has no object id, POST/DELETE return no content, and there is no GET for
// the link itself — presence is read from the parent workspace's scopeIds.
func associationSpec() ResourceSpec {
	return ResourceSpec{
		Name: "admin_workspace_scope_mapping",
		Endpoint: EndpointSpec{
			UriBase: "/workspaces/{workspace_id}/scopes/{scope_id}",
		},
		Association: &AssociationSpec{
			ReadPath:        "/workspaces/{workspace_id}",
			MemberPath:      "scopeIds",
			MemberAttribute: "scope_id",
		},
		Attributes: []AttributeSpec{
			{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
			{Name: "scope_id", Type: "string", Required: true, ForceNew: true},
		},
	}
}

var assocObjType = tftypes.Object{AttributeTypes: map[string]tftypes.Type{
	"id":           tftypes.String,
	"workspace_id": tftypes.String,
	"scope_id":     tftypes.String,
}}

func assocRaw(id string) tftypes.Value {
	idVal := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	if id != "" {
		idVal = tftypes.NewValue(tftypes.String, id)
	}
	return tftypes.NewValue(assocObjType, map[string]tftypes.Value{
		"id":           idVal,
		"workspace_id": tftypes.NewValue(tftypes.String, "ws-1"),
		"scope_id":     tftypes.NewValue(tftypes.String, "sc-9"),
	})
}

func assocResource(t *testing.T, srvURL string) (*dynamicResource, rschema.Schema) {
	t.Helper()
	spec := associationSpec()
	if err := spec.validate(); err != nil {
		t.Fatalf("spec must be valid: %v", err)
	}
	ep, err := spec.BuildEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	client, err := duplosdk.NewClient(srvURL, "tok", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &dynamicResource{spec: spec, endpoint: ep}
	r.Client = client

	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	return r, sr.Schema
}

func stateString(t *testing.T, raw tftypes.Value, attr string) string {
	t.Helper()
	m := map[string]tftypes.Value{}
	if err := raw.As(&m); err != nil {
		t.Fatal(err)
	}
	var s string
	_ = m[attr].As(&s)
	return s
}

// Create POSTs the link path itself — both ids in the URL, no body — and must
// tolerate the empty 200 the endpoint returns. The normal decode path rejects a
// body with no "data", so this would fail without CreateNoContent.
func TestAssociation_CreatePostsLinkPathAndIgnoresEmptyBody(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotMethod, gotPath = req.Method, req.URL.Path
		w.WriteHeader(http.StatusOK) // no content, exactly like the real API
	}))
	defer srv.Close()

	r, schema := assocResource(t, srv.URL)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schema}}
	r.Create(context.Background(),
		resource.CreateRequest{Plan: tfsdk.Plan{Schema: schema, Raw: assocRaw("")}}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create diags: %v", resp.Diagnostics)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/workspaces/ws-1/scopes/sc-9"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	if got := stateString(t, resp.State.Raw, "id"); got != "ws-1/sc-9" {
		t.Errorf("id = %q, want the path parameters joined with no trailing segment", got)
	}
}

// Read reports the link as present while the scope id is in the parent's list.
func TestAssociation_ReadKeepsStateWhenMemberPresent(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		_, _ = w.Write([]byte(`{"data":{"id":"ws-1","scopeIds":["other","sc-9"]}}`))
	}))
	defer srv.Close()

	r, schema := assocResource(t, srv.URL)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}
	r.Read(context.Background(),
		resource.ReadRequest{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read diags: %v", resp.Diagnostics)
	}
	// The PARENT is read, not the link path — the link has no GET.
	if want := "/workspaces/ws-1"; gotPath != want {
		t.Errorf("read path = %s, want %s", gotPath, want)
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("state was removed although the scope is still attached")
	}
	if got := stateString(t, resp.State.Raw, "scope_id"); got != "sc-9" {
		t.Errorf("scope_id = %q, want it re-asserted from the id (this is what makes import work)", got)
	}
	if got := stateString(t, resp.State.Raw, "workspace_id"); got != "ws-1" {
		t.Errorf("workspace_id = %q", got)
	}
}

// Import: `terraform import <ws>/<scope>` passes the id through and leaves every
// attribute null, so Read is what has to populate them — from the id alone. This
// is the shape TestAssociation_ReadKeepsStateWhenMemberPresent cannot exercise,
// because it seeds state with the attributes already set.
func TestAssociation_ReadPopulatesAttributesFromIDOnImport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"ws-1","scopeIds":["sc-9"]}}`))
	}))
	defer srv.Close()

	// Exactly what ImportStatePassthroughID leaves behind: id set, rest null.
	imported := tftypes.NewValue(assocObjType, map[string]tftypes.Value{
		"id":           tftypes.NewValue(tftypes.String, "ws-1/sc-9"),
		"workspace_id": tftypes.NewValue(tftypes.String, nil),
		"scope_id":     tftypes.NewValue(tftypes.String, nil),
	})

	r, schema := assocResource(t, srv.URL)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: schema, Raw: imported}}
	r.Read(context.Background(),
		resource.ReadRequest{State: tfsdk.State{Schema: schema, Raw: imported}}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read diags: %v", resp.Diagnostics)
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("state was removed although the scope is attached")
	}
	if got := stateString(t, resp.State.Raw, "workspace_id"); got != "ws-1" {
		t.Errorf("workspace_id = %q, want it recovered from the id", got)
	}
	if got := stateString(t, resp.State.Raw, "scope_id"); got != "sc-9" {
		t.Errorf("scope_id = %q, want it recovered from the id", got)
	}
}

// Detached out of band ⇒ the resource must leave state so the next plan
// recreates it, rather than reporting a mapping that no longer exists.
func TestAssociation_ReadRemovesStateWhenMemberGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"ws-1","scopeIds":["other"]}}`))
	}))
	defer srv.Close()

	r, schema := assocResource(t, srv.URL)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}
	r.Read(context.Background(),
		resource.ReadRequest{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read diags: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("state must be removed once the scope is no longer attached")
	}
}

// Parent gone ⇒ the link cannot exist either.
func TestAssociation_ReadRemovesStateWhenParentMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r, schema := assocResource(t, srv.URL)
	resp := resource.ReadResponse{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}
	r.Read(context.Background(),
		resource.ReadRequest{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read diags: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("state must be removed when the parent workspace is gone")
	}
}

// Delete targets the link path with NO "/{id}" appended — the link has no
// object id, and the default item path would produce a trailing slash.
func TestAssociation_DeleteTargetsLinkPath(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotMethod, gotPath = req.Method, req.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r, schema := assocResource(t, srv.URL)
	resp := resource.DeleteResponse{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}
	r.Delete(context.Background(),
		resource.DeleteRequest{State: tfsdk.State{Schema: schema, Raw: assocRaw("ws-1/sc-9")}}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete diags: %v", resp.Diagnostics)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if want := "/workspaces/ws-1/scopes/sc-9"; gotPath != want {
		t.Errorf("path = %s, want %s (no id segment appended)", gotPath, want)
	}
}

// The spec must reject association resources that cannot work: no way to detect
// the link, or an attribute that is not a required + forceNew path parameter.
func TestAssociation_ValidationRejectsBadSpecs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mutir func(*ResourceSpec)
	}{
		{"no readPath", func(s *ResourceSpec) { s.Association.ReadPath = "" }},
		{"no memberPath", func(s *ResourceSpec) { s.Association.MemberPath = "" }},
		{"no memberAttribute", func(s *ResourceSpec) { s.Association.MemberAttribute = "" }},
		{"unknown memberAttribute", func(s *ResourceSpec) { s.Association.MemberAttribute = "nope" }},
		{"attribute not a path param", func(s *ResourceSpec) {
			s.Attributes = append(s.Attributes, AttributeSpec{
				Name: "extra", Type: "string", Required: true, ForceNew: true, APIPath: "extra",
			})
		}},
		{"attribute not forceNew", func(s *ResourceSpec) { s.Attributes[1].ForceNew = false }},
		{"attribute optional", func(s *ResourceSpec) {
			s.Attributes[1].Required = false
			s.Attributes[1].Optional = true
		}},
		{"update declared", func(s *ResourceSpec) { s.Endpoint.Update = &OperationSpec{Verb: "PUT"} }},
		{"waiter declared", func(s *ResourceSpec) { s.Waiter = &WaiterSpec{} }},
		{"data source requested", func(s *ResourceSpec) { s.DataSource = true }},
		{"readPath references a non-path-parameter", func(s *ResourceSpec) {
			s.Association.ReadPath = "/workspaces/{workspace}" // typo: not {workspace_id}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := associationSpec()
			tc.mutir(&s)
			if err := s.validate(); err == nil {
				t.Error("expected a validation error, got nil")
			}
		})
	}
}

// A spec with no association keeps the conventional "/{id}" item path — this
// feature must not change any existing resource.
func TestAssociation_NonAssociationKeepsItemPath(t *testing.T) {
	spec := ResourceSpec{
		Name: "thing", IDPath: "id",
		Endpoint:   EndpointSpec{UriBase: "/things"},
		Attributes: []AttributeSpec{{Name: "name", Type: "string", Required: true, APIPath: "name"}},
	}
	ep, err := spec.BuildEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if ep.NoItemPath {
		t.Error("NoItemPath must stay false for a normal resource")
	}
}
