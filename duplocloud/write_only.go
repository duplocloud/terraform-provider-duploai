package duplocloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// ── Write-only arguments (Terraform 1.11+) ───────────────────────────────────
//
// A WriteOnly attribute is an alternative to an existing state-stored secret
// attribute: the value comes from config, goes into the request, and never
// reaches plan or state (the framework nulls it in every response). Because it
// cannot be diffed, its VersionAttribute decides when it is sent: on create
// whenever it is set, on update only when the version changed. The existing
// attribute is untouched, so configs on older Terraform keep working as before.

// writeOnlyAttrs returns the spec's WriteOnly attributes.
func (r *dynamicResource) writeOnlyAttrs() []AttributeSpec {
	var out []AttributeSpec
	for _, a := range r.spec.Attributes {
		if a.WriteOnly {
			out = append(out, a)
		}
	}
	return out
}

// writeOnlyVersionChanged reports, per WriteOnly attribute, whether its version
// attribute differs between plan and prior state — i.e. whether this update
// must send the value.
func (r *dynamicResource) writeOnlyVersionChanged(plan, state tftypes.Value) map[string]bool {
	var p, s map[string]tftypes.Value
	if plan.As(&p) != nil || state.As(&s) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, a := range r.writeOnlyAttrs() {
		pv, sv := p[a.VersionAttribute], s[a.VersionAttribute]
		if !pv.IsNull() && !pv.Equal(sv) {
			out[a.Name] = true
		}
	}
	return out
}

// addWriteOnlyValues writes the config value of each WriteOnly attribute that
// send selects into the request body, at its create or update path.
func (r *dynamicResource) addWriteOnlyValues(config tftypes.Value, verb string, body map[string]any, send func(AttributeSpec) bool) {
	var top map[string]tftypes.Value
	if body == nil || config.As(&top) != nil {
		return
	}
	for _, a := range r.writeOnlyAttrs() {
		if !send(a) {
			continue
		}
		p := a.effectiveCreatePath()
		if verb == "update" {
			p = a.effectiveUpdatePath()
		}
		if p == "" {
			continue
		}
		if val, ok := attrToRequest(a, top[a.Name]); ok {
			setPath(body, strings.Split(p, "."), val)
		}
	}
}

// writeOnlyInUsePaths returns the update paths of the WriteOnly attributes in use
// (version set in the plan). mergeIntoCurrent keeps the stored value at these
// paths instead of deleting it as "planned null": a write-only secret is always
// null in the plan, and an update that is not rotating it must leave it exactly
// as stored. Deleting it made the backend see the secret as removed — flipping a
// skill-provisioned resource to Processing — before its hook restored the stored
// value, after which no reconcile was dispatched and the resource stayed
// Processing (seen live on RDS and ElastiCache).
func (r *dynamicResource) writeOnlyInUsePaths(plan map[string]tftypes.Value) map[string]bool {
	out := map[string]bool{}
	for _, a := range r.writeOnlyAttrs() {
		if v, ok := plan[a.VersionAttribute]; ok && !v.IsNull() {
			if p := a.effectiveUpdatePath(); p != "" {
				out[p] = true
			}
		}
	}
	return out
}

// resendOmittedWriteOnly writes the config value of each ResendWhenOmitted
// write-only attribute in use (its update path in inUse) into merged when the
// current record carries no real value there: absent, empty, or masked with
// redactedPrefix. A record that still returns the secret keeps it untouched.
func (r *dynamicResource) resendOmittedWriteOnly(merged map[string]any, config tftypes.Value, inUse map[string]bool) {
	var top map[string]tftypes.Value
	if config.As(&top) != nil {
		return
	}
	for _, a := range r.writeOnlyAttrs() {
		p := a.effectiveUpdatePath()
		if !a.ResendWhenOmitted || p == "" || !inUse[p] {
			continue
		}
		segs := strings.Split(p, ".")
		if cur, ok := lookupPath(merged, segs); ok && cur != nil {
			if s, isStr := cur.(string); !isStr || (s != "" && !strings.HasPrefix(s, redactedPrefix)) {
				continue
			}
		}
		if val, ok := attrToRequest(a, top[a.Name]); ok {
			setPath(merged, segs, val)
		}
	}
}

// keepShadowedNull holds null, in a refreshed state, every attribute that shares
// a WriteOnly attribute's create path while that write-only alternative is in use
// (its version is set): such an attribute is null in the prior state because the
// user moved the secret to the write-only argument, and reading the API's echo
// of the secret into it would put the secret back into state and show a diff.
func (r *dynamicResource) keepShadowedNull(prior, next tftypes.Value) tftypes.Value {
	wo := r.writeOnlyAttrs()
	if len(wo) == 0 {
		return next
	}
	var p, n map[string]tftypes.Value
	if prior.As(&p) != nil || next.As(&n) != nil {
		return next
	}
	changed := false
	for _, w := range wo {
		if v, ok := p[w.VersionAttribute]; !ok || v.IsNull() || !v.IsKnown() {
			continue
		}
		for _, a := range r.spec.Attributes {
			if a.WriteOnly || a.effectiveCreatePath() != w.effectiveCreatePath() {
				continue
			}
			if pv, ok := p[a.Name]; ok && pv.IsNull() && !n[a.Name].IsNull() {
				n[a.Name] = tftypes.NewValue(n[a.Name].Type(), nil)
				changed = true
			}
		}
	}
	if !changed {
		return next
	}
	return tftypes.NewValue(next.Type(), n)
}

// writeOnlyValidator requires a WriteOnly value and its version together: a value
// with no version could never send a later change (silently ignored), and a
// version with no value would keep the write-only handling active for a secret
// that is no longer configured.
type writeOnlyValidator struct{ spec ResourceSpec }

func (v writeOnlyValidator) Description(_ context.Context) string {
	return "Requires a write-only attribute and its version attribute to be set together."
}

func (v writeOnlyValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v writeOnlyValidator) ValidateResource(_ context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	for _, a := range v.spec.Attributes {
		if !a.WriteOnly {
			continue
		}
		valueSet, versionSet := !configAttrNull(req.Config.Raw, a.Name), !configAttrNull(req.Config.Raw, a.VersionAttribute)
		if valueSet && !versionSet {
			resp.Diagnostics.AddAttributeError(path.Root(a.VersionAttribute), "Missing "+a.VersionAttribute,
				fmt.Sprintf("%s is write-only, so Terraform cannot detect when it changes. Set %s (for example to 1) "+
					"and increase it whenever %s changes; only then is the new value sent.", a.Name, a.VersionAttribute, a.Name))
		}
		// A version with no value would make updates re-send the stored secret
		// as if the write-only argument were still in use.
		if versionSet && !valueSet {
			resp.Diagnostics.AddAttributeError(path.Root(a.VersionAttribute), "Missing "+a.Name,
				fmt.Sprintf("%s versions %s; set %s too, or remove %s.", a.VersionAttribute, a.Name, a.Name, a.VersionAttribute))
		}
	}
}
