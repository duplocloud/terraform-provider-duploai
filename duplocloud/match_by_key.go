package duplocloud

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// matchByKey pairs a list(object)'s response elements with the base value (the
// plan on create/update, prior state on refresh) by a nested string key instead
// of by index. It exists for backends that append server-generated elements the
// user never configured (admin_provider: an AWS role credential gets a generated
// dataEx "externalId"): with index pairing the extra element changes the list
// length, so apply reports an inconsistent result and refresh shows a diff that
// can never converge.
//
// alignResponseByKey rewrites the raw response before it is converted to state:
// at each list(object) carrying MatchByKey whose base is known and non-null, the
// response elements are reordered to the base's key order and elements whose key
// is not in the base are dropped. With no usable base (import, attribute unset,
// unknown keys) the response is returned unchanged, so nothing is hidden there.
// Nested objects and lists of objects are walked so a MatchByKey list inside
// another list is aligned too; outer lists are paired by index only when their
// lengths agree.
func alignResponseByKey(a AttributeSpec, base tftypes.Value, data any) any {
	if !hasMatchByKey(a) || data == nil || !base.IsKnown() || base.IsNull() {
		return data
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" {
		return data
	}
	switch info.coll {
	case "":
		return alignObjectByKey(a.Attributes, base, data)
	case "list":
		var baseElems []tftypes.Value
		if err := base.As(&baseElems); err != nil {
			return data
		}
		arr, ok := data.([]any)
		if !ok {
			return data
		}
		if a.MatchByKey != "" {
			if aligned, ok := alignListByKey(a, baseElems, arr); ok {
				arr = aligned
			}
		}
		if len(arr) != len(baseElems) {
			return arr
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = alignObjectByKey(a.Attributes, baseElems[i], e)
		}
		return out
	default:
		return data
	}
}

// alignListByKey returns the response elements matching each base element's
// key, in base order. ok is false when any base key is not a known string, in
// which case the caller keeps the response as is.
func alignListByKey(a AttributeSpec, baseElems []tftypes.Value, arr []any) ([]any, bool) {
	segs := strings.Split(a.matchKeyResponseKey(), ".")
	used := make([]bool, len(arr))
	out := make([]any, 0, len(baseElems))
	for _, be := range baseElems {
		var bm map[string]tftypes.Value
		if !be.IsKnown() || be.IsNull() || be.As(&bm) != nil {
			return nil, false
		}
		kv, present := bm[a.MatchByKey]
		var key string
		if !present || !kv.IsKnown() || kv.IsNull() || kv.As(&key) != nil {
			return nil, false
		}
		for i, e := range arr {
			if !used[i] && toStringValue(extractPath(e, segs)) == key {
				used[i] = true
				out = append(out, e)
				break
			}
		}
	}
	return out, true
}

func alignObjectByKey(attrs []AttributeSpec, base tftypes.Value, data any) any {
	dm, ok := data.(map[string]any)
	if !ok || !base.IsKnown() || base.IsNull() {
		return data
	}
	var bm map[string]tftypes.Value
	if base.As(&bm) != nil {
		return data
	}
	var out map[string]any
	for _, na := range attrs {
		if !hasMatchByKey(na) {
			continue
		}
		key := na.responsePath()
		if key == "" {
			key = na.Name
		}
		if strings.Contains(key, ".") {
			continue // only direct children are rewritten; dotted paths are left alone
		}
		child, present := dm[key]
		bv, hasBase := bm[na.Name]
		if !present || !hasBase {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(dm))
			for k, v := range dm {
				out[k] = v
			}
		}
		out[key] = alignResponseByKey(na, bv, child)
	}
	if out == nil {
		return data
	}
	return out
}

// hasMatchByKey reports whether a, or any attribute nested below it, sets MatchByKey.
func hasMatchByKey(a AttributeSpec) bool {
	if a.MatchByKey != "" {
		return true
	}
	for _, na := range a.Attributes {
		if hasMatchByKey(na) {
			return true
		}
	}
	return false
}

// matchKeyResponseKey returns the JSON path of the MatchByKey attribute inside a
// response element.
func (a AttributeSpec) matchKeyResponseKey() string {
	for _, na := range a.Attributes {
		if na.Name == a.MatchByKey {
			if rp := na.responsePath(); rp != "" {
				return rp
			}
			return na.Name
		}
	}
	return a.MatchByKey
}

// copyValueMap returns a shallow copy of m. tftypes.Value.As can hand back the
// value's own backing map, so writing to it would mutate the source value (here:
// the request plan that ModifyPlan pins computed outputs into).
func copyValueMap(m map[string]tftypes.Value) map[string]tftypes.Value {
	out := make(map[string]tftypes.Value, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
