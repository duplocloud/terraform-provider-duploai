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

// pruneStateOnlyKeyed returns state with every element of a MatchByKey list
// whose key does not appear in the matching plan list removed, at any nesting
// level. ModifyPlan uses it to decide whether a plan differs from state only by
// server-added elements (e.g. the generated externalId that import puts in
// state with no base to align against). changed reports whether anything was
// removed. When the plan side is unknown, null or carries an unknown key, or
// outer lists differ in length, the value is returned unchanged so nothing is
// hidden.
func pruneStateOnlyKeyed(a AttributeSpec, state, plan tftypes.Value) (tftypes.Value, bool) {
	if !hasMatchByKey(a) || !state.IsKnown() || state.IsNull() || !plan.IsKnown() || plan.IsNull() {
		return state, false
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" {
		return state, false
	}
	switch info.coll {
	case "":
		return pruneObjectStateOnlyKeyed(a.Attributes, state, plan)
	case "list":
		var sElems, pElems []tftypes.Value
		if state.As(&sElems) != nil || plan.As(&pElems) != nil {
			return state, false
		}
		changed := false
		if a.MatchByKey != "" {
			planKeys := map[string]bool{}
			for _, pe := range pElems {
				k, ok := objectStringField(pe, a.MatchByKey)
				if !ok {
					return state, false
				}
				planKeys[k] = true
			}
			kept := make([]tftypes.Value, 0, len(sElems))
			for _, se := range sElems {
				if k, ok := objectStringField(se, a.MatchByKey); ok && !planKeys[k] {
					changed = true
					continue
				}
				kept = append(kept, se)
			}
			sElems = kept
		}
		sElems = append([]tftypes.Value(nil), sElems...) // As may share the value's backing slice
		if len(sElems) == len(pElems) {
			for i := range sElems {
				if nv, c := pruneObjectStateOnlyKeyed(a.Attributes, sElems[i], pElems[i]); c {
					sElems[i], changed = nv, true
				}
			}
		}
		if !changed {
			return state, false
		}
		return tftypes.NewValue(state.Type(), sElems), true
	default:
		return state, false
	}
}

func pruneObjectStateOnlyKeyed(attrs []AttributeSpec, state, plan tftypes.Value) (tftypes.Value, bool) {
	if !state.IsKnown() || state.IsNull() || !plan.IsKnown() || plan.IsNull() {
		return state, false
	}
	var sm, pm map[string]tftypes.Value
	if state.As(&sm) != nil || plan.As(&pm) != nil {
		return state, false
	}
	sm = copyValueMap(sm) // As may share the value's backing map
	changed := false
	for _, na := range attrs {
		sv, ok1 := sm[na.Name]
		pv, ok2 := pm[na.Name]
		if !ok1 || !ok2 {
			continue
		}
		if nv, c := pruneStateOnlyKeyed(na, sv, pv); c {
			sm[na.Name], changed = nv, true
		}
	}
	if !changed {
		return state, false
	}
	return tftypes.NewValue(state.Type(), sm), true
}

// objectStringField returns obj[name] when it is a known, non-null string.
func objectStringField(obj tftypes.Value, name string) (string, bool) {
	var m map[string]tftypes.Value
	if !obj.IsKnown() || obj.IsNull() || obj.As(&m) != nil {
		return "", false
	}
	v, ok := m[name]
	var s string
	if !ok || !v.IsKnown() || v.IsNull() || v.As(&s) != nil {
		return "", false
	}
	return s, true
}

// maskUnsetComputed replaces, inside v, every computed leaf whose config value
// is null with unknown, pairing list elements with config by MatchByKey (or by
// index when no key is set and lengths agree). Used only for the ModifyPlan
// comparison: an unset optional+computed leaf (e.g. a credential field's
// type/is_sensitive) carries whatever the plan modifiers copied from state by
// index, which says nothing about whether the user changed anything.
func maskUnsetComputed(a AttributeSpec, v, cfg tftypes.Value) tftypes.Value {
	if !v.IsKnown() || v.IsNull() {
		return v
	}
	if cfg.IsNull() {
		if a.Computed {
			return tftypes.NewValue(v.Type(), tftypes.UnknownValue)
		}
		return v
	}
	if !cfg.IsKnown() || len(a.Attributes) == 0 {
		return v
	}
	info, _ := parseType(a.Type)
	switch info.coll {
	case "":
		return maskObjectUnsetComputed(a.Attributes, v, cfg)
	case "list":
		var vElems, cElems []tftypes.Value
		if v.As(&vElems) != nil || cfg.As(&cElems) != nil {
			return v
		}
		out := make([]tftypes.Value, len(vElems))
		for i, ve := range vElems {
			out[i] = ve
			var ce *tftypes.Value
			if a.MatchByKey != "" {
				if k, ok := objectStringField(ve, a.MatchByKey); ok {
					for j := range cElems {
						if ck, ok := objectStringField(cElems[j], a.MatchByKey); ok && ck == k {
							ce = &cElems[j]
							break
						}
					}
				}
			} else if len(vElems) == len(cElems) {
				ce = &cElems[i]
			}
			if ce != nil {
				out[i] = maskObjectUnsetComputed(a.Attributes, ve, *ce)
			}
		}
		return tftypes.NewValue(v.Type(), out)
	default:
		return v
	}
}

func maskObjectUnsetComputed(attrs []AttributeSpec, v, cfg tftypes.Value) tftypes.Value {
	var vm, cm map[string]tftypes.Value
	if !v.IsKnown() || v.IsNull() || !cfg.IsKnown() || cfg.IsNull() || v.As(&vm) != nil || cfg.As(&cm) != nil {
		return v
	}
	vm = copyValueMap(vm)
	for _, na := range attrs {
		cv, ok1 := cm[na.Name]
		sv, ok2 := vm[na.Name]
		if ok1 && ok2 {
			vm[na.Name] = maskUnsetComputed(na, sv, cv)
		}
	}
	return tftypes.NewValue(v.Type(), vm)
}

// copyValueMap returns a shallow copy of m. tftypes.Value.As can hand back the
// value's own backing map, so writing to it would mutate the source value (here:
// the prior state the plan is pinned from).
func copyValueMap(m map[string]tftypes.Value) map[string]tftypes.Value {
	out := make(map[string]tftypes.Value, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
