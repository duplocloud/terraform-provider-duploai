package duplocloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

const woSecret = "s3cret-value"

// woHarness is a resource with a state-stored secret (password), its
// write-only alternative (password_wo + password_wo_version), a plain input and
// a computed output, served by a mock API that records every request body and
// echoes the stored password back on GET — like RDS, which decrypts on read.
type woHarness struct {
	t      *testing.T
	r      *dynamicResource
	schema schema.Schema
	typ    tftypes.Object
	mu     sync.Mutex
	bodies []string // "METHOD body"
}

func newWOHarness(t *testing.T) *woHarness {
	h := &woHarness{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, req.Method+" "+string(b))
		h.mu.Unlock()
		_, _ = w.Write([]byte(`{"data":{"id":"obj1","updatedAt":"t2","spec":{"description":"d","password":"` + woSecret + `"}}}`))
	}))
	t.Cleanup(srv.Close)
	client, err := duplosdk.NewClient(srv.URL, "tok", false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	h.r = &dynamicResource{
		baseResource: baseResource{Client: client},
		endpoint:     duplosdk.Endpoint{UriBase: "/v1/items/{workspace_id}", Update: duplosdk.Operation{Verb: http.MethodPut}},
		spec: ResourceSpec{
			Name: "wo", IDPath: "id", Endpoint: EndpointSpec{UriBase: "/v1/items/{workspace_id}"},
			Attributes: []AttributeSpec{
				{Name: "workspace_id", Type: "string", Required: true, ForceNew: true},
				{Name: "description", Type: "string", Optional: true, APIPath: "spec.description"},
				{Name: "password", Type: "string", Optional: true, Sensitive: true, APIPath: "spec.password"},
				{Name: "password_wo", Type: "string", Optional: true, Sensitive: true, WriteOnly: true,
					VersionAttribute: "password_wo_version", CreatePath: "spec.password", UpdatePath: "spec.password"},
				{Name: "password_wo_version", Type: "int", Optional: true},
				{Name: "updated_at", Type: "string", Computed: true, APIPath: "updatedAt"},
			},
		},
	}
	if err := h.r.spec.validate(); err != nil {
		t.Fatalf("harness spec must validate: %v", err)
	}
	var sr resource.SchemaResponse
	h.r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	h.schema = sr.Schema
	h.typ = sr.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	return h
}

// val builds an object of the resource type; unspecified attributes are null.
func (h *woHarness) val(set map[string]tftypes.Value) tftypes.Value {
	vals := map[string]tftypes.Value{}
	for n, typ := range h.typ.AttributeTypes {
		vals[n] = tftypes.NewValue(typ, nil)
	}
	vals["workspace_id"] = tftypes.NewValue(tftypes.String, "ws1")
	for k, v := range set {
		vals[k] = v
	}
	return tftypes.NewValue(h.typ, vals)
}

func (h *woHarness) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.bodies...)
}

func num(n int) tftypes.Value   { return tftypes.NewValue(tftypes.Number, n) }
func unknownStr() tftypes.Value { return tftypes.NewValue(tftypes.String, tftypes.UnknownValue) }
func attrOf(v tftypes.Value, n string) tftypes.Value {
	m := map[string]tftypes.Value{}
	_ = v.As(&m)
	return m[n]
}

// Create sends the write-only value from config (the plan never carries it),
// and nothing about it reaches state.
func TestWriteOnly_CreateSendsConfigValueNotState(t *testing.T) {
	h := newWOHarness(t)
	cfg := h.val(map[string]tftypes.Value{"password_wo": str(woSecret), "password_wo_version": num(1), "description": str("d")})
	plan := h.val(map[string]tftypes.Value{"password_wo_version": num(1), "description": str("d"), "id": unknownStr(), "updated_at": unknownStr()})
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: h.schema}}
	h.r.Create(context.Background(), resource.CreateRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	reqs := h.requests()
	if len(reqs) == 0 || !strings.HasPrefix(reqs[0], "POST") || !strings.Contains(reqs[0], `"password":"`+woSecret+`"`) {
		t.Fatalf("POST must carry the write-only value, got %v", reqs)
	}
	st := resp.State.Raw
	if !attrOf(st, "password_wo").IsNull() || !attrOf(st, "password").IsNull() {
		t.Errorf("no secret may reach state: password_wo=%v password=%v", attrOf(st, "password_wo"), attrOf(st, "password"))
	}
	if !attrOf(st, "password_wo_version").Equal(num(1)) {
		t.Errorf("version must be kept in state, got %v", attrOf(st, "password_wo_version"))
	}
}

func (h *woHarness) update(state, plan, cfg tftypes.Value) *resource.UpdateResponse {
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: h.schema}}
	h.r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
		State:  tfsdk.State{Schema: h.schema, Raw: state},
	}, resp)
	if resp.Diagnostics.HasError() {
		h.t.Fatalf("Update: %v", resp.Diagnostics)
	}
	return resp
}

// An unrelated update leaves the secret out of the PUT (the backends keep the
// current one); a changed version sends it, even when nothing else changed —
// which must not be mistaken for a no-op and skipped.
func TestWriteOnly_UpdateSendsOnlyOnVersionChange(t *testing.T) {
	h := newWOHarness(t)
	state := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(1), "updated_at": str("t1")})

	// Description changes, version does not: no secret in the PUT.
	cfg := h.val(map[string]tftypes.Value{"password_wo": str(woSecret), "password_wo_version": num(1), "description": str("d2")})
	plan := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d2"), "password_wo_version": num(1), "updated_at": unknownStr()})
	h.update(state, plan, cfg)
	reqs := h.requests()
	if len(reqs) != 1 || !strings.HasPrefix(reqs[0], "PUT") || strings.Contains(reqs[0], woSecret) {
		t.Fatalf("unrelated update must PUT without the secret, got %v", reqs)
	}

	// Only the version changes: a PUT carrying the secret.
	cfg = h.val(map[string]tftypes.Value{"password_wo": str(woSecret), "password_wo_version": num(2), "description": str("d")})
	plan = h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(2), "updated_at": unknownStr()})
	resp := h.update(state, plan, cfg)
	reqs = h.requests()
	if len(reqs) != 2 || !strings.HasPrefix(reqs[1], "PUT") || !strings.Contains(reqs[1], `"password":"`+woSecret+`"`) {
		t.Fatalf("version change must PUT the secret, got %v", reqs)
	}
	if !attrOf(resp.State.Raw, "password").IsNull() || !attrOf(resp.State.Raw, "password_wo").IsNull() {
		t.Errorf("no secret may reach state after update")
	}
}

// With the write-only alternative in use, a refresh must not read the API's echo
// of the secret into the state-stored attribute (RDS returns it decrypted).
// Without it, the old attribute keeps today's behaviour.
func TestWriteOnly_RefreshKeepsShadowedAttributeNull(t *testing.T) {
	h := newWOHarness(t)
	ctx := context.Background()
	read := func(prior tftypes.Value) tftypes.Value {
		resp := &resource.ReadResponse{State: tfsdk.State{Schema: h.schema, Raw: prior}}
		h.r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: h.schema, Raw: prior}}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read: %v", resp.Diagnostics)
		}
		return resp.State.Raw
	}
	got := read(h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "password_wo_version": num(1)}))
	if !attrOf(got, "password").IsNull() {
		t.Errorf("write-only in use: password must stay null, got %v", attrOf(got, "password"))
	}
	// Not using the write-only argument: unchanged behaviour, the API value is read.
	got = read(h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "password": str("old")}))
	if !attrOf(got, "password").Equal(str(woSecret)) {
		t.Errorf("without write-only, password refreshes from the API as before, got %v", attrOf(got, "password"))
	}
}

// A version change is a real update: computed outputs must not be pinned to
// their prior values, or the post-apply state would contradict the plan.
func TestWriteOnly_VersionChangeIsRealUpdateInPlan(t *testing.T) {
	h := newWOHarness(t)
	state := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(1), "updated_at": str("t1")})
	plan := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(2), "updated_at": unknownStr()})
	cfg := h.val(map[string]tftypes.Value{"description": str("d"), "password_wo": str(woSecret), "password_wo_version": num(2)})
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: h.schema, Raw: plan}}
	h.r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: h.schema, Raw: cfg},
		Plan:   tfsdk.Plan{Schema: h.schema, Raw: plan},
		State:  tfsdk.State{Schema: h.schema, Raw: state},
	}, resp)
	if attrOf(resp.Plan.Raw, "updated_at").IsKnown() {
		t.Errorf("updated_at was pinned to %v on a version change", attrOf(resp.Plan.Raw, "updated_at"))
	}
}

func TestWriteOnly_ValidatorRequiresVersion(t *testing.T) {
	h := newWOHarness(t)
	check := func(set map[string]tftypes.Value) bool {
		resp := &resource.ValidateConfigResponse{}
		writeOnlyValidator{spec: h.r.spec}.ValidateResource(context.Background(),
			resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: h.schema, Raw: h.val(set)}}, resp)
		return resp.Diagnostics.HasError()
	}
	if !check(map[string]tftypes.Value{"password_wo": str(woSecret)}) {
		t.Error("password_wo without password_wo_version must be rejected")
	}
	if !check(map[string]tftypes.Value{"password_wo_version": num(1)}) {
		t.Error("password_wo_version without password_wo must be rejected")
	}
	if check(map[string]tftypes.Value{"password_wo": str(woSecret), "password_wo_version": num(1)}) || check(nil) {
		t.Error("with a version, or with neither set, the config is valid")
	}
}

func TestWriteOnlySpecValidation(t *testing.T) {
	base := `{"name":"probe","description":"probe","idPath":"id","endpoint":{"uriBase":"/v3/things"},"attributes":[`
	cases := []struct{ name, attrs, wantErr string }{
		{"valid", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"versionAttribute":"v","createPath":"spec.p"},{"name":"v","type":"int","optional":true}`, ""},
		{"no version", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"createPath":"spec.p"}`, "requires versionAttribute"},
		{"apiPath reads it back", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"versionAttribute":"v","apiPath":"spec.p"},{"name":"v","type":"int","optional":true}`, "request paths only"},
		{"computed", `{"name":"p_wo","type":"string","optional":true,"computed":true,"writeOnly":true,"versionAttribute":"v","createPath":"spec.p"},{"name":"v","type":"int","optional":true}`, "must be optional"},
		{"non-string", `{"name":"p_wo","type":"int","optional":true,"writeOnly":true,"versionAttribute":"v","createPath":"spec.p"},{"name":"v","type":"int","optional":true}`, "type string"},
		{"version has a path", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"versionAttribute":"v","createPath":"spec.p"},{"name":"v","type":"int","optional":true,"apiPath":"spec.v"}`, "must have no paths"},
		{"version is a string", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"versionAttribute":"v","createPath":"spec.p"},{"name":"v","type":"string","optional":true}`, "optional int"},
		{"version missing", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"versionAttribute":"nope","createPath":"spec.p"}`, "is not an attribute"},
		{"nested", `{"name":"o","type":"object","optional":true,"apiPath":"o","attributes":[{"name":"p","type":"string","optional":true,"writeOnly":true}]}`, "top-level attributes only"},
		{"resendWhenOmitted without merge", `{"name":"p_wo","type":"string","optional":true,"writeOnly":true,"resendWhenOmitted":true,"versionAttribute":"v","createPath":"spec.p"},{"name":"v","type":"int","optional":true}`, "requires mergeUpdateIntoCurrent"},
		{"resendWhenOmitted not writeOnly", `{"name":"p","type":"string","optional":true,"resendWhenOmitted":true,"apiPath":"spec.p"}`, "require writeOnly"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var spec ResourceSpec
			if err := json.Unmarshal([]byte(base+tc.attrs+`]}`), &spec); err != nil {
				t.Fatal(err)
			}
			err := spec.validate()
			if tc.wantErr == "" && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// Every shipped write-only attribute is WriteOnly in the resource schema and
// absent from the data source; its old alternative is unchanged in type and
// flags (the compatibility guarantee for Terraform < 1.11).
func TestWriteOnly_ShippedSpecs(t *testing.T) {
	ctx := context.Background()
	want := map[string]string{
		"rds_instance": "master_user_password", "rds_cluster": "master_user_password",
		"elasticache": "auth_token", "aws_secret": "secret_string",
		"azure_postgres_flexible_server": "administrator_password", "azure_key_vault_secret": "value",
	}
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, s := range specs {
		old, ok := want[s.Name]
		if !ok {
			continue
		}
		found++
		r := &dynamicResource{spec: s}
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		wo, ok := sr.Schema.Attributes[old+"_wo"].(schema.StringAttribute)
		if !ok || !wo.WriteOnly || !wo.Sensitive {
			t.Errorf("%s: %s_wo must be a sensitive write-only string", s.Name, old)
		}
		if v, ok := sr.Schema.Attributes[old+"_wo_version"].(schema.Int64Attribute); !ok || !v.Optional || v.Computed {
			t.Errorf("%s: %s_wo_version must be an optional int", s.Name, old)
		}
		if o, ok := sr.Schema.Attributes[old].(schema.StringAttribute); !ok || !o.Optional || o.WriteOnly || o.Computed {
			t.Errorf("%s: %s must stay an optional state-stored string", s.Name, old)
		}
		ep, err := s.BuildEndpoint()
		if err != nil {
			t.Fatal(err)
		}
		d := &dynamicDataSource{spec: s, endpoint: ep}
		var dr datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &dr)
		for _, n := range []string{old + "_wo", old + "_wo_version"} {
			if _, ok := dr.Schema.Attributes[n]; ok {
				t.Errorf("%s data source must not expose %s", s.Name, n)
			}
		}
	}
	if found != len(want) {
		t.Errorf("found %d of %d specs", found, len(want))
	}
}

// sendOnEveryUpdate (Key Vault): an unrelated update still carries the value,
// because the backend's value-less update path fails.
func TestWriteOnly_SendOnEveryUpdate(t *testing.T) {
	h := newWOHarness(t)
	h.r.spec.Attributes[3].SendOnEveryUpdate = true
	state := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(1), "updated_at": str("t1")})
	cfg := h.val(map[string]tftypes.Value{"password_wo": str(woSecret), "password_wo_version": num(1), "description": str("d2")})
	plan := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d2"), "password_wo_version": num(1), "updated_at": unknownStr()})
	h.update(state, plan, cfg)
	if reqs := h.requests(); len(reqs) != 1 || !strings.Contains(reqs[0], `"password":"`+woSecret+`"`) {
		t.Fatalf("sendOnEveryUpdate must send the value on an unrelated update, got %v", reqs)
	}
}

// With mergeUpdateIntoCurrent, an update that is not rotating the write-only
// secret sends the stored secret back unchanged (from the pre-update GET), so the
// backend sees no spec change; a rotation still sends the new value.
func TestWriteOnly_MergeKeepsStoredSecret(t *testing.T) {
	h := newWOHarness(t)
	h.r.spec.MergeUpdateIntoCurrent = true
	state := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(1), "updated_at": str("t1")})

	cfg := h.val(map[string]tftypes.Value{"password_wo": str("ignored"), "password_wo_version": num(1), "description": str("d2")})
	plan := h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d2"), "password_wo_version": num(1), "updated_at": unknownStr()})
	h.update(state, plan, cfg)
	put := ""
	for _, rq := range h.requests() {
		if strings.HasPrefix(rq, "PUT") {
			put = rq
		}
	}
	if !strings.Contains(put, `"password":"`+woSecret+`"`) {
		t.Fatalf("unrelated update must re-send the stored secret, got %s", put)
	}

	cfg = h.val(map[string]tftypes.Value{"password_wo": str("rotated"), "password_wo_version": num(2), "description": str("d")})
	plan = h.val(map[string]tftypes.Value{"id": str("ws1/obj1"), "description": str("d"), "password_wo_version": num(2), "updated_at": unknownStr()})
	h.update(state, plan, cfg)
	for _, rq := range h.requests() {
		if strings.HasPrefix(rq, "PUT") {
			put = rq
		}
	}
	if !strings.Contains(put, `"password":"rotated"`) {
		t.Fatalf("rotation must send the new value, got %s", put)
	}
}

// resendWhenOmitted: a record that omits, empties or masks the write-only
// secret gets the config value; a record that still returns it keeps it; an
// attribute not in use (no version) or without the flag is never filled.
func TestWriteOnly_ResendWhenOmitted(t *testing.T) {
	h := newWOHarness(t)
	a := &h.r.spec.Attributes[3]
	a.ResendWhenOmitted = true
	cfg := h.val(map[string]tftypes.Value{"password_wo": str("cfg"), "password_wo_version": num(1)})
	inUse := map[string]bool{a.effectiveUpdatePath(): true}
	segs := strings.Split(a.effectiveUpdatePath(), ".")
	for _, tc := range []struct {
		name   string
		stored any
		absent bool
		inUse  map[string]bool
		flag   bool
		want   any
	}{
		{"absent", nil, true, inUse, true, "cfg"},
		{"null", nil, false, inUse, true, "cfg"},
		{"empty", "", false, inUse, true, "cfg"},
		{"masked", redactedPrefix + "**", false, inUse, true, "cfg"},
		{"returned", "stored", false, inUse, true, "stored"},
		{"not in use", nil, true, map[string]bool{}, true, nil},
		{"flag off", nil, true, inUse, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.ResendWhenOmitted = tc.flag
			merged := map[string]any{}
			if !tc.absent {
				setPath(merged, segs, tc.stored)
			}
			h.r.resendOmittedWriteOnly(merged, cfg, tc.inUse)
			if got, _ := lookupPath(merged, segs); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Every shipped Sensitive attribute's body paths reach the endpoint, so the
// client redacts them in logs whatever the API key is called.
func TestSensitiveBodyPaths_ShippedSpecs(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"azure_key_vault_secret": "value", "rds_instance": "spec.masterUserPassword",
		"aws_secret": "spec.updateRequest.secretString", "azure_postgres_flexible_server": "spec.administratorLoginPassword"}
	for _, s := range specs {
		p, ok := want[s.Name]
		if !ok {
			continue
		}
		ep, err := s.BuildEndpoint()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, x := range ep.SensitivePaths {
			found = found || x == p
		}
		if !found {
			t.Errorf("%s: SensitivePaths %v lacks %s", s.Name, ep.SensitivePaths, p)
		}
	}
}
