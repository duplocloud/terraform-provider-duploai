package duplocloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// qaAdvRDSBackend is a fake AWSRdsInstances controller. Since BE #721
// (FACT-7) spec.masterUserPassword is omitted from every response. dropPUT
// makes the next PUT COMMIT and then lose its response (connection closed
// before any reply), the ambiguous-update case.
type qaAdvRDSBackend struct {
	mu       sync.Mutex
	spec     map[string]any
	password string // what the "cloud" holds
	puts     []map[string]any
	dropPUT  bool
}

// qaAdvRDSSnapshot is a copy of the backend's observable fields, taken under the
// lock: the HTTP handler goroutine may still be finishing (a hijacked PUT) when
// the test reads them.
type qaAdvRDSSnapshot struct {
	password string
	puts     []map[string]any
}

func (b *qaAdvRDSBackend) snap() qaAdvRDSSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return qaAdvRDSSnapshot{password: b.password, puts: append([]map[string]any(nil), b.puts...)}
}

func (b *qaAdvRDSBackend) setDropPUT() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dropPUT = true
}

func (b *qaAdvRDSBackend) handle(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		spec, _ := body["spec"].(map[string]any)
		if pw, ok := spec["masterUserPassword"].(string); ok && pw != "" {
			b.password = pw
		}
		cp := map[string]any{}
		for k, v := range spec {
			if k != "masterUserPassword" {
				cp[k] = v
			}
		}
		b.spec = cp
		if r.Method == http.MethodPut {
			b.puts = append(b.puts, body)
			if b.dropPUT {
				b.dropPUT = false
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					_ = conn.Close() // committed, reply lost
					return
				}
			}
		}
	}
	out, _ := json.Marshal(map[string]any{"data": map[string]any{
		"id": "db1", "name": "db1", "status": "Complete", "spec": b.spec,
	}})
	_, _ = w.Write(out)
}

const qaAdvRDS = "duploai_rds_instance"

func qaAdvRDSConfig(h *qaAdvHarness, pw tftypes.Value, ver tftypes.Value) tftypes.Value {
	return h.obj(qaAdvRDS, map[string]tftypes.Value{
		"workspace_id":                    tftypes.NewValue(tftypes.String, "ws1"),
		"name":                            tftypes.NewValue(tftypes.String, "db1"),
		"scope_ids":                       tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "s1")}),
		"resource_group_id":               tftypes.NewValue(tftypes.String, "rg1"),
		"environment_id":                  tftypes.NewValue(tftypes.String, "env1"),
		"engine":                          tftypes.NewValue(tftypes.String, "postgres"),
		"db_instance_class":               tftypes.NewValue(tftypes.String, "db.t3.micro"),
		"master_user_password_wo":         pw,
		"master_user_password_wo_version": ver,
	})
}

type qaAdvStep struct {
	plan        *tfprotov6.DynamicValue
	planDiags   []*tfprotov6.Diagnostic
	applyDiags  []*tfprotov6.Diagnostic
	newState    *tfprotov6.DynamicValue
	newPrivate  []byte
	newIdentity *tfprotov6.ResourceIdentityData
}

// plan runs PlanResourceChange like Terraform core.
func (h *qaAdvHarness) plan(name string, prior tftypes.Value, priorPriv []byte, priorID *tfprotov6.ResourceIdentityData, cfg tftypes.Value) *tfprotov6.PlanResourceChangeResponse {
	h.t.Helper()
	var proposed *tfprotov6.DynamicValue
	if prior.IsNull() {
		proposed = h.dv(name, cfg)
	} else {
		proposed = qaAdvProposed(h.t, h, name, prior, cfg)
	}
	pr, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: name, PriorState: h.dv(name, prior), ProposedNewState: proposed, Config: h.dv(name, cfg),
		PriorPrivate: priorPriv, PriorIdentity: priorID,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return pr
}

// qaAdvConforms mirrors core's final-plan check: every value known in the
// first plan must be unchanged in the apply-time plan.
func qaAdvConforms(first, final tftypes.Value) []string {
	var fm, gm map[string]tftypes.Value
	_ = first.As(&fm)
	_ = final.As(&gm)
	var bad []string
	for k, v := range fm {
		if v.IsFullyKnown() && !v.Equal(gm[k]) {
			bad = append(bad, k)
		}
	}
	return bad
}

// step runs plan (planCfg may hold unknowns), then — as Terraform core does
// during apply — re-plans with the now-known applyCfg, checks the final plan
// conforms to the first, and applies it.
func (h *qaAdvHarness) step(name string, prior tftypes.Value, priorPriv []byte, priorID *tfprotov6.ResourceIdentityData, planCfg, applyCfg tftypes.Value) qaAdvStep {
	h.t.Helper()
	pr := h.plan(name, prior, priorPriv, priorID, planCfg)
	s := qaAdvStep{plan: pr.PlannedState, planDiags: pr.Diagnostics}
	if qaAdvErr(pr.Diagnostics) != "" {
		return s
	}
	final := h.plan(name, prior, priorPriv, priorID, applyCfg)
	s.planDiags = append(s.planDiags, final.Diagnostics...)
	if qaAdvErr(final.Diagnostics) != "" {
		return s
	}
	if bad := qaAdvConforms(h.val(name, pr.PlannedState), h.val(name, final.PlannedState)); len(bad) > 0 {
		s.planDiags = append(s.planDiags, &tfprotov6.Diagnostic{Severity: tfprotov6.DiagnosticSeverityError,
			Summary: "Provider produced inconsistent final plan", Detail: strings.Join(bad, ",")})
		return s
	}
	ar, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: name, PriorState: h.dv(name, prior), PlannedState: final.PlannedState, Config: h.dv(name, applyCfg),
		PlannedPrivate: final.PlannedPrivate, PlannedIdentity: final.PlannedIdentity,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	s.applyDiags, s.newState, s.newPrivate, s.newIdentity = ar.Diagnostics, ar.NewState, ar.Private, ar.NewIdentity
	return s
}

func (h *qaAdvHarness) val(name string, d *tfprotov6.DynamicValue) tftypes.Value {
	v, err := d.Unmarshal(h.resType(name))
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func qaAdvInt(v tftypes.Value, attr string) string {
	var m map[string]tftypes.Value
	_ = v.As(&m)
	return m[attr].String()
}

// Scenario: the PUT that rotates master_user_password_wo commits in the
// backend but its response is lost. The failed apply must leave the prior
// version in state (so the next plan re-sends), never put the secret in
// state, and the retried apply must send the same secret again.
func TestQAAdv_WriteOnly_LostRotationResponse(t *testing.T) {
	be := &qaAdvRDSBackend{}
	h := newQAAdvHarness(t, be.handle)
	null := tftypes.NewValue(h.resType(qaAdvRDS), nil)
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	num := func(n int) tftypes.Value { return tftypes.NewValue(tftypes.Number, n) }

	c1 := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-1"), num(1))
	s1 := h.step(qaAdvRDS, null, nil, nil, c1, c1)
	if e := qaAdvErr(append(s1.planDiags, s1.applyDiags...)); e != "" {
		t.Fatalf("create: %s", e)
	}
	st1 := h.val(qaAdvRDS, s1.newState)
	if be.snap().password != "PLACEHOLDER-pw-1" {
		t.Fatalf("create did not send the write-only password (backend has %q)", be.snap().password)
	}

	// Rotate; the PUT commits but the reply is lost.
	c2 := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-2"), num(2))
	be.setDropPUT()
	s2 := h.step(qaAdvRDS, st1, s1.newPrivate, s1.newIdentity, c2, c2)
	if qaAdvErr(s2.planDiags) != "" {
		t.Fatalf("rotate plan: %s", qaAdvErr(s2.planDiags))
	}
	if qaAdvErr(s2.applyDiags) == "" {
		t.Fatalf("lost PUT response must surface as an error")
	}
	if be.snap().password != "PLACEHOLDER-pw-2" || len(be.snap().puts) != 1 {
		t.Fatalf("setup: PUT should have committed pw-2 once (backend %q, puts %d)", be.snap().password, len(be.snap().puts))
	}
	after := h.val(qaAdvRDS, s2.newState)
	if got := qaAdvInt(after, "master_user_password_wo_version"); !strings.Contains(got, "1") {
		t.Errorf("failed rotation recorded version %s in state; want the prior version 1 so the next plan re-sends", got)
	}
	for label, v := range map[string]tftypes.Value{"after create": st1, "after failed rotate": after} {
		if strings.Contains(v.String(), "PLACEHOLDER-pw") {
			t.Errorf("%s: write-only secret present in state: %s", label, v)
		}
	}

	// Recovery: same config, normal backend → plans the rotation again and
	// re-sends the same password exactly once.
	s3 := h.step(qaAdvRDS, after, s2.newPrivate, s2.newIdentity, c2, c2)
	if e := qaAdvErr(append(s3.planDiags, s3.applyDiags...)); e != "" {
		t.Fatalf("recovery: %s", e)
	}
	if len(be.snap().puts) != 2 {
		t.Fatalf("recovery should issue exactly one more PUT, got %d total", len(be.snap().puts))
	}
	sp, _ := be.snap().puts[1]["spec"].(map[string]any)
	if sp["masterUserPassword"] != "PLACEHOLDER-pw-2" {
		t.Errorf("recovery PUT did not re-send the rotated password: %v", sp["masterUserPassword"])
	}
	final := h.val(qaAdvRDS, s3.newState)
	if got := qaAdvInt(final, "master_user_password_wo_version"); !strings.Contains(got, "2") {
		t.Errorf("version after recovery = %s, want 2", got)
	}
	if strings.Contains(final.String(), "PLACEHOLDER-pw") {
		t.Errorf("write-only secret present in final state")
	}
}

// Scenario: _wo unknown at plan time (from another resource), version bumped.
// The plan must pass validation and the apply must send the value known by then.
func TestQAAdv_WriteOnly_UnknownValueAtPlan(t *testing.T) {
	be := &qaAdvRDSBackend{}
	h := newQAAdvHarness(t, be.handle)
	null := tftypes.NewValue(h.resType(qaAdvRDS), nil)
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	num := func(n int) tftypes.Value { return tftypes.NewValue(tftypes.Number, n) }
	unk := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

	c1 := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-1"), num(1))
	s1 := h.step(qaAdvRDS, null, nil, nil, c1, c1)
	if e := qaAdvErr(append(s1.planDiags, s1.applyDiags...)); e != "" {
		t.Fatalf("create: %s", e)
	}
	vr, err := h.server.ValidateResourceConfig(h.ctx, &tfprotov6.ValidateResourceConfigRequest{
		TypeName: qaAdvRDS, Config: h.dv(qaAdvRDS, qaAdvRDSConfig(h, unk, num(2))),
		ClientCapabilities: &tfprotov6.ValidateResourceConfigClientCapabilities{WriteOnlyAttributesAllowed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := qaAdvErr(vr.Diagnostics); e != "" {
		t.Fatalf("validate with unknown _wo: %s", e)
	}
	s2 := h.step(qaAdvRDS, h.val(qaAdvRDS, s1.newState), s1.newPrivate, s1.newIdentity,
		qaAdvRDSConfig(h, unk, num(2)), qaAdvRDSConfig(h, str("PLACEHOLDER-pw-2"), num(2)))
	if e := qaAdvErr(append(s2.planDiags, s2.applyDiags...)); e != "" {
		t.Fatalf("rotate: %s", e)
	}
	if be.snap().password != "PLACEHOLDER-pw-2" {
		t.Errorf("apply did not send the write-only value that was unknown at plan time (backend %q)", be.snap().password)
	}
}

// Scenario: _wo_version unknown at plan time (e.g. derived from another
// resource) that resolves at apply to the version already in state. Plan
// treats unknown as "changed" (computed outputs not pinned); the apply then
// finds nothing changed and persists the PLAN — which must still be a fully
// known state, or Terraform core rejects it ("Provider returned invalid
// result object after apply").
func TestQAAdv_WriteOnly_UnknownVersionResolvesUnchanged(t *testing.T) {
	be := &qaAdvRDSBackend{}
	h := newQAAdvHarness(t, be.handle)
	null := tftypes.NewValue(h.resType(qaAdvRDS), nil)
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	num := func(n int) tftypes.Value { return tftypes.NewValue(tftypes.Number, n) }

	c1 := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-1"), num(1))
	s1 := h.step(qaAdvRDS, null, nil, nil, c1, c1)
	if e := qaAdvErr(append(s1.planDiags, s1.applyDiags...)); e != "" {
		t.Fatalf("create: %s", e)
	}
	planCfg := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-1"), tftypes.NewValue(tftypes.Number, tftypes.UnknownValue))
	s2 := h.step(qaAdvRDS, h.val(qaAdvRDS, s1.newState), s1.newPrivate, s1.newIdentity, planCfg, c1)
	if e := qaAdvErr(append(s2.planDiags, s2.applyDiags...)); e != "" {
		t.Fatalf("apply: %s", e)
	}
	ns := h.val(qaAdvRDS, s2.newState)
	if !ns.IsFullyKnown() {
		var m map[string]tftypes.Value
		_ = ns.As(&m)
		var unknown []string
		for k, v := range m {
			if !v.IsFullyKnown() {
				unknown = append(unknown, k)
			}
		}
		t.Errorf("apply returned a state with unknown values %v (the no-op skip persisted the plan); "+
			"Terraform core fails this apply. PUTs sent: %d", unknown, len(be.snap().puts))
	}
}

// Retry scenario through the engine: an update of a mergeUpdateIntoCurrent
// spec reads the object first via GetWithRetry (60 s window). If the object
// 404s there and the user hits Ctrl-C, the apply must stop promptly, send no
// PUT, and keep the prior state.
func TestQAAdv_MergeIntoCurrent_404CancelHonoured(t *testing.T) {
	be := &qaAdvRDSBackend{}
	var gone atomic.Bool
	h := newQAAdvHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if gone.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		be.handle(w, r)
	})
	null := tftypes.NewValue(h.resType(qaAdvRDS), nil)
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	num := func(n int) tftypes.Value { return tftypes.NewValue(tftypes.Number, n) }
	c1 := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-1"), num(1))
	s1 := h.step(qaAdvRDS, null, nil, nil, c1, c1)
	if e := qaAdvErr(append(s1.planDiags, s1.applyDiags...)); e != "" {
		t.Fatalf("create: %s", e)
	}
	st1 := h.val(qaAdvRDS, s1.newState)
	c2 := qaAdvRDSConfig(h, str("PLACEHOLDER-pw-2"), num(2))
	pr := h.plan(qaAdvRDS, st1, s1.newPrivate, s1.newIdentity, c2)
	if e := qaAdvErr(pr.Diagnostics); e != "" {
		t.Fatalf("plan: %s", e)
	}
	gone.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	start := time.Now()
	ar, err := h.server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName: qaAdvRDS, PriorState: h.dv(qaAdvRDS, st1), PlannedState: pr.PlannedState, Config: h.dv(qaAdvRDS, c2),
		PlannedPrivate: pr.PlannedPrivate, PlannedIdentity: pr.PlannedIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("Ctrl-C during the pre-update read did not stop the apply promptly: %s", el)
	}
	if qaAdvErr(ar.Diagnostics) == "" {
		t.Errorf("want an error when the object is gone before update")
	}
	if len(be.snap().puts) != 0 {
		t.Errorf("no PUT may be sent after the pre-update read failed, got %d", len(be.snap().puts))
	}
	if got := h.val(qaAdvRDS, ar.NewState); !got.Equal(st1) {
		t.Errorf("failed update must keep the prior state")
	}
}
