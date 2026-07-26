package duplocloud

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// orderByKey gives a list(object) attribute a canonical element order so a
// backend that returns the elements in a different order than the user declared
// them does not surface as order-only drift. It works in two places that must
// agree: the read side (sortObjectsByKey, applied in attrFromResponse before the
// response is stored in state) and the plan side (orderByKeyModifier, applied to
// the planned config). Both sort lexically by the same nested string key, so
// prior state and planned config are always compared in the same order.

// orderKeyResponseKey returns the JSON path used to read the sort key out of a
// response element for a list(object) whose OrderByKey names a nested attribute.
func (a AttributeSpec) orderKeyResponseKey() string {
	for _, na := range a.Attributes {
		if na.Name == a.OrderByKey {
			if rp := na.responsePath(); rp != "" {
				return rp
			}
			return na.Name
		}
	}
	return a.OrderByKey
}

// sortObjectsByKey stably sorts decoded response objects by the string value at
// keyPath. Elements whose key is missing sort as the empty string, keeping the
// sort total and deterministic.
func sortObjectsByKey(arr []any, keyPath string) []any {
	if len(arr) < 2 || keyPath == "" {
		return arr
	}
	segs := strings.Split(keyPath, ".")
	out := make([]any, len(arr))
	copy(out, arr)
	sort.SliceStable(out, func(i, j int) bool {
		return toStringValue(extractPath(out[i], segs)) < toStringValue(extractPath(out[j], segs))
	})
	return out
}

// orderByKeyModifier is a list plan modifier that reorders the planned elements
// by a nested string key, matching the read side's canonical order.
type orderByKeyModifier struct{ key string }

func (m orderByKeyModifier) Description(_ context.Context) string {
	return fmt.Sprintf("Sorts list elements by %q so ordering is canonical and order-only diffs never appear.", m.key)
}

func (m orderByKeyModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m orderByKeyModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	elems := req.PlanValue.Elements()
	if len(elems) < 2 {
		return
	}
	// Only sort when every element's key is a known, non-null string; otherwise
	// the order is ambiguous, so leave the plan untouched.
	keys := make([]string, len(elems))
	for i, e := range elems {
		k, ok := objectStringAttr(e, m.key)
		if !ok {
			return
		}
		keys[i] = k
	}
	idx := make([]int, len(elems))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
	// Already in canonical order — nothing to do (avoids needless plan churn).
	inOrder := true
	for i := range idx {
		if idx[i] != i {
			inOrder = false
			break
		}
	}
	if inOrder {
		return
	}
	out := make([]attr.Value, len(elems))
	for i, j := range idx {
		out[i] = elems[j]
	}
	sorted, d := types.ListValue(req.PlanValue.ElementType(ctx), out)
	resp.Diagnostics.Append(d...)
	if !resp.Diagnostics.HasError() {
		resp.PlanValue = sorted
	}
}

// objectStringAttr returns the string value of a named attribute on an object
// element, and whether it was present, known, and non-null.
func objectStringAttr(v attr.Value, name string) (string, bool) {
	obj, ok := v.(types.Object)
	if !ok || obj.IsNull() || obj.IsUnknown() {
		return "", false
	}
	av, ok := obj.Attributes()[name]
	if !ok {
		return "", false
	}
	s, ok := av.(types.String)
	if !ok || s.IsNull() || s.IsUnknown() {
		return "", false
	}
	return s.ValueString(), true
}
