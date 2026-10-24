package duplocloud

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// recomputeWhenChangedModifier implements AttributeSpec.RecomputeWhenChanged:
// on an update where one of the named inputs changes and this attribute is
// unset in config, it plans the attribute as unknown instead of the prior value
// UseStateForUnknown kept. An unknown value is left out of the request body, so
// the backend derives it again from the new inputs.
type recomputeWhenChangedModifier struct{ inputs []string }

func (m recomputeWhenChangedModifier) Description(_ context.Context) string {
	return "Recomputed by the API when " + strings.Join(m.inputs, ", ") + " changes and it is not set in config."
}

func (m recomputeWhenChangedModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m recomputeWhenChangedModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || !req.ConfigValue.IsNull() {
		return // create, destroy, or the user set the value
	}
	var p, s, c map[string]tftypes.Value
	if req.Plan.Raw.As(&p) != nil || req.State.Raw.As(&s) != nil || req.Config.Raw.As(&c) != nil {
		return
	}
	for _, in := range m.inputs {
		pv, ok := p[in]
		if !ok {
			continue
		}
		if pv.IsFullyKnown() {
			if !pv.Equal(s[in]) {
				resp.PlanValue = types.StringUnknown()
				return
			}
			continue
		}
		// Unknown in the plan. Modifiers see the plan as proposed, so an
		// optional+computed input left unset in config is unknown here even
		// though its own UseStateForUnknown keeps the prior value: unchanged.
		// Set in config to an unknown value (a reference): it may change.
		if cv, ok := c[in]; ok && !cv.IsNull() {
			resp.PlanValue = types.StringUnknown()
			return
		}
	}
}
