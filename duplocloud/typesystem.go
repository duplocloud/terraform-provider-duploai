package duplocloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// This file is the engine's type system. It covers the full set of Terraform
// Plugin Framework types so that adding a resource never requires extending the
// engine:
//
//   - primitives: string, bool, int, number
//   - collections of any element: list(...), set(...), map(...)
//   - nested objects: object (+ nested attributes), and list/set/map of object
//
// Objects nest arbitrarily — a nested attribute is itself an AttributeSpec, so
// the same machinery recurses to any depth.

// typeInfo is a parsed AttributeSpec.Type: an optional collection wrapper around
// an element kind.
type typeInfo struct {
	coll string // "", "list", "set", "map"
	elem string // "string", "bool", "int", "number", "object"
}

func isElementKind(s string) bool {
	switch s {
	case "string", "bool", "int", "number", "object":
		return true
	}
	return false
}

// parseType parses a type string such as "string", "list(number)", or
// "map(object)".
func parseType(s string) (typeInfo, error) {
	for _, c := range []string{"list", "set", "map"} {
		prefix := c + "("
		if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, ")") {
			elem := s[len(prefix) : len(s)-1]
			if !isElementKind(elem) {
				return typeInfo{}, fmt.Errorf("unsupported element type %q", elem)
			}
			return typeInfo{coll: c, elem: elem}, nil
		}
	}
	if !isElementKind(s) {
		return typeInfo{}, fmt.Errorf("unsupported type %q", s)
	}
	return typeInfo{elem: s}, nil
}

// primitiveAttrType maps an element kind to its framework attr.Type, used as the
// ElementType of typed (non-nested) collections.
func primitiveAttrType(elem string) attr.Type {
	switch elem {
	case "string":
		return types.StringType
	case "bool":
		return types.BoolType
	case "int":
		return types.Int64Type
	case "number":
		return types.Float64Type
	}
	return nil
}

// ── Schema building ─────────────────────────────────────────────────────────

// attrSchema builds the framework schema.Attribute for one AttributeSpec,
// recursing through nested objects.
func attrSchema(a AttributeSpec) schema.Attribute {
	info, _ := parseType(a.Type) // validated earlier
	if info.elem == "object" {
		return objectSchema(a, info)
	}
	if info.coll == "" {
		return primitiveSchema(a, info.elem)
	}
	return primitiveCollectionSchema(a, info)
}

func primitiveSchema(a AttributeSpec, elem string) schema.Attribute {
	switch elem {
	case "string":
		o := schema.StringAttribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			var s string
			_ = json.Unmarshal(*a.Default, &s)
			o.Default = stringdefault.StaticString(s)
		}
		if len(a.OneOf) > 0 {
			o.Validators = []validator.String{stringvalidator.OneOf(a.OneOf...)}
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, stringplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, stringplanmodifier.RequiresReplace())
		}
		return o
	case "bool":
		o := schema.BoolAttribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			var b bool
			_ = json.Unmarshal(*a.Default, &b)
			o.Default = booldefault.StaticBool(b)
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, boolplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, boolplanmodifier.RequiresReplace())
		}
		return o
	case "int":
		o := schema.Int64Attribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			var i int64
			_ = json.Unmarshal(*a.Default, &i)
			o.Default = int64default.StaticInt64(i)
		}
		if a.Min != nil {
			o.Validators = append(o.Validators, int64validator.AtLeast(int64(*a.Min)))
		}
		if a.Max != nil {
			o.Validators = append(o.Validators, int64validator.AtMost(int64(*a.Max)))
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, int64planmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, int64planmodifier.RequiresReplace())
		}
		return o
	default: // number
		o := schema.Float64Attribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			var f float64
			_ = json.Unmarshal(*a.Default, &f)
			o.Default = float64default.StaticFloat64(f)
		}
		if a.Min != nil {
			o.Validators = append(o.Validators, float64validator.AtLeast(*a.Min))
		}
		if a.Max != nil {
			o.Validators = append(o.Validators, float64validator.AtMost(*a.Max))
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, float64planmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, float64planmodifier.RequiresReplace())
		}
		return o
	}
}

// useStateForUnknown reports whether a computed attribute should keep its prior
// state value when the config does not change it, instead of going unknown
// ("known after apply") on every update. This is essential for computed+forceNew
// attributes — otherwise an unknown planned value combined with RequiresReplace
// spuriously forces replacement on unrelated changes — and is the standard
// behavior for optional+computed (server-defaulted) attributes. It is
// deliberately NOT applied to pure-output computed fields (e.g. status), which
// legitimately change on each apply and must stay recomputed — unless the spec
// marks one Stable (e.g. the resource's own ID), which never changes.
func useStateForUnknown(a AttributeSpec) bool {
	// preserveTarget: a computed sibling of a PreserveUnmanagedInto attribute
	// must keep its prior-state value at plan time so the union-on-write can
	// re-send the server-managed entries it holds.
	return a.Computed && (a.Optional || a.ForceNew || a.preserveTarget || a.Stable)
}

// staticDefaultValue decodes a spec's raw JSON `default` into a framework
// attr.Value of type at, so it can be wired as a list/set/map/object default
// (the primitive equivalents use stringdefault.StaticString and friends). It
// reuses goToTftypesValue — the same JSON→framework bridge the response path
// relies on — and decodes with UseNumber so large integers in the default keep
// full precision. It returns false (no default wired) when the JSON is
// malformed or does not conform to the attribute's type, matching the lenient
// stance the primitive path takes on a failed unmarshal.
func staticDefaultValue(ctx context.Context, at attr.Type, raw *json.RawMessage) (attr.Value, bool) {
	dec := json.NewDecoder(bytes.NewReader(*raw))
	dec.UseNumber()
	var g any
	if err := dec.Decode(&g); err != nil {
		return nil, false
	}
	v, err := at.ValueFromTerraform(ctx, goToTftypesValue(at.TerraformType(ctx), g))
	if err != nil {
		return nil, false
	}
	return v, true
}

func primitiveCollectionSchema(a AttributeSpec, info typeInfo) schema.Attribute {
	et := primitiveAttrType(info.elem)
	switch info.coll {
	case "set":
		o := schema.SetAttribute{ElementType: et, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = setdefault.StaticValue(v.(types.Set))
			}
		}
		if a.MinItems > 0 {
			o.Validators = append(o.Validators, setvalidator.SizeAtLeast(a.MinItems))
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, setplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, setplanmodifier.RequiresReplace())
		}
		return o
	case "map":
		o := schema.MapAttribute{ElementType: et, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = mapdefault.StaticValue(v.(types.Map))
			}
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, mapplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, mapplanmodifier.RequiresReplace())
		}
		return o
	default: // list
		o := schema.ListAttribute{ElementType: et, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = listdefault.StaticValue(v.(types.List))
			}
		}
		if a.MinItems > 0 {
			o.Validators = append(o.Validators, listvalidator.SizeAtLeast(a.MinItems))
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, listplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, listplanmodifier.RequiresReplace())
		}
		return o
	}
}

func nestedAttrMap(attrs []AttributeSpec) map[string]schema.Attribute {
	out := make(map[string]schema.Attribute, len(attrs))
	for _, na := range attrs {
		out[na.Name] = attrSchema(na)
	}
	return out
}

func objectSchema(a AttributeSpec, info typeInfo) schema.Attribute {
	nested := nestedAttrMap(a.Attributes)
	switch info.coll {
	case "list":
		o := schema.ListNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: nested}, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = listdefault.StaticValue(v.(types.List))
			}
		}
		if a.MinItems > 0 {
			o.Validators = append(o.Validators, listvalidator.SizeAtLeast(a.MinItems))
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, listplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, listplanmodifier.RequiresReplace())
		}
		return o
	case "set":
		o := schema.SetNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: nested}, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = setdefault.StaticValue(v.(types.Set))
			}
		}
		if a.MinItems > 0 {
			o.Validators = append(o.Validators, setvalidator.SizeAtLeast(a.MinItems))
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, setplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, setplanmodifier.RequiresReplace())
		}
		return o
	case "map":
		o := schema.MapNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: nested}, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = mapdefault.StaticValue(v.(types.Map))
			}
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, mapplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, mapplanmodifier.RequiresReplace())
		}
		return o
	default: // single object
		o := schema.SingleNestedAttribute{Attributes: nested, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated}
		if a.Default != nil {
			if v, ok := staticDefaultValue(context.Background(), o.GetType(), a.Default); ok {
				o.Default = objectdefault.StaticValue(v.(types.Object))
			}
		}
		if useStateForUnknown(a) {
			o.PlanModifiers = append(o.PlanModifiers, objectplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			o.PlanModifiers = append(o.PlanModifiers, objectplanmodifier.RequiresReplace())
		}
		return o
	}
}

// ── Generic value bridges (used for everything that is not a nested object) ───

// tftypesToGo converts any framework value into a JSON-ready Go value.
func tftypesToGo(v tftypes.Value) any {
	if v.IsNull() || !v.IsKnown() {
		return nil
	}
	t := v.Type()
	switch {
	case t.Is(tftypes.String):
		var s string
		_ = v.As(&s)
		return s
	case t.Is(tftypes.Bool):
		var b bool
		_ = v.As(&b)
		return b
	case t.Is(tftypes.Number):
		var f big.Float
		_ = v.As(&f)
		return bigFloatToJSONNumber(&f)
	}
	switch t.(type) {
	case tftypes.List, tftypes.Set, tftypes.Tuple:
		var elems []tftypes.Value
		_ = v.As(&elems)
		out := make([]any, 0, len(elems))
		for _, e := range elems {
			out = append(out, tftypesToGo(e))
		}
		return out
	case tftypes.Map, tftypes.Object:
		var m map[string]tftypes.Value
		_ = v.As(&m)
		out := make(map[string]any, len(m))
		for k, e := range m {
			out[k] = tftypesToGo(e)
		}
		return out
	}
	return nil
}

// goToTftypesValue converts a JSON-decoded Go value into a framework value of
// the given type, recursing through collections and structurally through
// objects/maps.
func goToTftypesValue(t tftypes.Type, g any) tftypes.Value {
	if g == nil {
		return tftypes.NewValue(t, nil)
	}
	switch {
	case t.Is(tftypes.String):
		return tftypes.NewValue(t, toStringValue(g))
	case t.Is(tftypes.Bool):
		b, _ := g.(bool)
		return tftypes.NewValue(t, b)
	case t.Is(tftypes.Number):
		return tftypes.NewValue(t, toBigFloat(g))
	}
	switch tt := t.(type) {
	case tftypes.List:
		arr := toAnySlice(g)
		elems := make([]tftypes.Value, 0, len(arr))
		for _, e := range arr {
			elems = append(elems, goToTftypesValue(tt.ElementType, e))
		}
		return tftypes.NewValue(t, elems)
	case tftypes.Set:
		arr := toAnySlice(g)
		elems := make([]tftypes.Value, 0, len(arr))
		for _, e := range arr {
			elems = append(elems, goToTftypesValue(tt.ElementType, e))
		}
		return tftypes.NewValue(t, elems)
	case tftypes.Map:
		m := toAnyMap(g)
		out := make(map[string]tftypes.Value, len(m))
		for k, e := range m {
			out[k] = goToTftypesValue(tt.ElementType, e)
		}
		return tftypes.NewValue(t, out)
	case tftypes.Object:
		m := toAnyMap(g)
		out := make(map[string]tftypes.Value, len(tt.AttributeTypes))
		for name, at := range tt.AttributeTypes {
			out[name] = goToTftypesValue(at, m[name])
		}
		return tftypes.NewValue(t, out)
	}
	return tftypes.NewValue(t, nil)
}

// ── Spec-driven object mapping (for per-field name remapping via apiPath) ─────

// attrToRequest converts a plan value into a JSON-ready Go value, honoring the
// per-field request paths of nested objects.
func attrToRequest(a AttributeSpec, v tftypes.Value) (any, bool) {
	if v.IsNull() || !v.IsKnown() {
		return nil, false
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" {
		return tftypesToGo(v), true
	}
	switch info.coll {
	case "":
		return objectToRequest(a.Attributes, v), true
	case "map":
		var m map[string]tftypes.Value
		_ = v.As(&m)
		out := make(map[string]any, len(m))
		for k, e := range m {
			out[k] = objectToRequest(a.Attributes, e)
		}
		return out, true
	default: // list, set
		var elems []tftypes.Value
		_ = v.As(&elems)
		out := make([]any, 0, len(elems))
		for _, e := range elems {
			out = append(out, objectToRequest(a.Attributes, e))
		}
		return out, true
	}
}

func objectToRequest(attrs []AttributeSpec, v tftypes.Value) map[string]any {
	if v.IsNull() || !v.IsKnown() {
		return nil
	}
	var m map[string]tftypes.Value
	_ = v.As(&m)
	obj := map[string]any{}
	for _, na := range attrs {
		if na.NoSend {
			continue
		}
		child, ok := m[na.Name]
		if !ok {
			continue
		}
		val, ok := attrToRequest(na, child)
		if !ok {
			continue
		}
		key := na.requestPath()
		if key == "" {
			key = na.Name
		}
		setPath(obj, strings.Split(key, "."), val)
	}
	return obj
}

// attrFromResponse converts a response value into a framework value of type t,
// honoring the per-field response paths of nested objects.
// normalizeCsvOrder sorts the comma-separated tokens of s into a stable lexical
// order. Used for order-insensitive backend fields (e.g. bootstrap broker
// strings) that the API returns in non-deterministic order. An empty string is
// returned unchanged.
func normalizeCsvOrder(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, ",")
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func attrFromResponse(a AttributeSpec, t tftypes.Type, data any) tftypes.Value {
	if data == nil {
		return tftypes.NewValue(t, nil)
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" {
		if a.NormalizeCsvOrder {
			if s, ok := data.(string); ok {
				data = normalizeCsvOrder(s)
			}
		}
		// Honor filterResponseKeys here too (not only in the top-level loop) so a
		// map(string) attribute nested inside an object (e.g. azure.tags) still
		// drops server-injected keys before it reaches state.
		if len(a.FilterResponseKeys) > 0 {
			data = filterMapKeys(data, a.FilterResponseKeys)
		}
		return goToTftypesValue(t, data)
	}
	if info.coll == "" {
		return objectFromResponse(a.Attributes, t, data)
	}
	switch tt := t.(type) {
	case tftypes.List:
		arr := toAnySlice(data)
		elems := make([]tftypes.Value, 0, len(arr))
		for _, e := range arr {
			elems = append(elems, objectFromResponse(a.Attributes, tt.ElementType, e))
		}
		return tftypes.NewValue(t, elems)
	case tftypes.Set:
		arr := toAnySlice(data)
		elems := make([]tftypes.Value, 0, len(arr))
		for _, e := range arr {
			elems = append(elems, objectFromResponse(a.Attributes, tt.ElementType, e))
		}
		return tftypes.NewValue(t, elems)
	case tftypes.Map:
		m := toAnyMap(data)
		out := make(map[string]tftypes.Value, len(m))
		for k, e := range m {
			out[k] = objectFromResponse(a.Attributes, tt.ElementType, e)
		}
		return tftypes.NewValue(t, out)
	}
	return tftypes.NewValue(t, nil)
}

func objectFromResponse(attrs []AttributeSpec, t tftypes.Type, data any) tftypes.Value {
	ot, ok := t.(tftypes.Object)
	if !ok {
		return tftypes.NewValue(t, nil)
	}
	dm, _ := data.(map[string]any)
	out := make(map[string]tftypes.Value, len(ot.AttributeTypes))
	for _, na := range attrs {
		at := ot.AttributeTypes[na.Name]
		var childData any
		if dm != nil {
			key := na.responsePath()
			if key == "" {
				key = na.Name
			}
			childData = extractPath(dm, strings.Split(key, "."))
		}
		out[na.Name] = attrFromResponse(na, at, childData)
	}
	return tftypes.NewValue(t, out)
}

// mergeUnknownFromResponse keeps the known leaves of a plan value and fills only
// the still-unknown ones from the API response. It is used on create/update so a
// computed child nested inside a configured object (e.g. a server-populated
// list) gets resolved to a known value, without overwriting the sibling values
// the user configured. respData is the response value at the attribute's path.
func mergeUnknownFromResponse(a AttributeSpec, t tftypes.Type, plan tftypes.Value, respData any) tftypes.Value {
	if plan.IsFullyKnown() {
		return plan
	}
	if !plan.IsKnown() {
		// The whole value is unknown — take it from the response.
		return attrFromResponse(a, t, respData)
	}
	// Known at this level but with unknown descendants: only single nested
	// objects need a per-field merge; anything else is rebuilt from the response.
	info, _ := parseType(a.Type)
	ot, isObj := t.(tftypes.Object)
	if info.elem != "object" || info.coll != "" || !isObj {
		return attrFromResponse(a, t, respData)
	}
	var cur map[string]tftypes.Value
	if err := plan.As(&cur); err != nil {
		return attrFromResponse(a, t, respData)
	}
	dm, _ := respData.(map[string]any)
	out := make(map[string]tftypes.Value, len(ot.AttributeTypes))
	for _, na := range a.Attributes {
		ct := ot.AttributeTypes[na.Name]
		var childResp any
		if dm != nil {
			key := na.responsePath()
			if key == "" {
				key = na.Name
			}
			childResp = extractPath(dm, strings.Split(key, "."))
		}
		out[na.Name] = mergeUnknownFromResponse(na, ct, cur[na.Name], childResp)
	}
	return tftypes.NewValue(t, out)
}

func toAnySlice(g any) []any        { s, _ := g.([]any); return s }
func toAnyMap(g any) map[string]any { m, _ := g.(map[string]any); return m }

// bigFloatToJSONNumber renders a number as a json.Number so it marshals into the
// request body without float64 rounding — integers keep full precision.
func bigFloatToJSONNumber(f *big.Float) json.Number {
	if f.IsInt() {
		return json.Number(f.Text('f', 0))
	}
	return json.Number(f.Text('g', -1))
}

// ── Data source schema building ──────────────────────────────────────────────
//
// These functions mirror the resource schema builders above but use
// datasource/schema types, which have no plan modifiers or defaults.
// The caller adjusts Required/Optional/Computed on each AttributeSpec before
// passing it in — see dynamicDataSource.Schema() for the adjustment rules.

func dsAttrSchema(a AttributeSpec) dsschema.Attribute {
	info, _ := parseType(a.Type)
	if info.elem == "object" {
		return dsObjectSchema(a, info)
	}
	if info.coll == "" {
		return dsPrimitiveSchema(a, info.elem)
	}
	return dsPrimitiveCollectionSchema(a, info)
}

func dsPrimitiveSchema(a AttributeSpec, elem string) dsschema.Attribute {
	switch elem {
	case "string":
		o := dsschema.StringAttribute{
			Required: a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
		if len(a.OneOf) > 0 {
			o.Validators = []validator.String{stringvalidator.OneOf(a.OneOf...)}
		}
		return o
	case "bool":
		return dsschema.BoolAttribute{
			Required: a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	case "int":
		return dsschema.Int64Attribute{
			Required: a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	default: // number
		return dsschema.Float64Attribute{
			Required: a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	}
}

func dsPrimitiveCollectionSchema(a AttributeSpec, info typeInfo) dsschema.Attribute {
	et := primitiveAttrType(info.elem)
	switch info.coll {
	case "set":
		return dsschema.SetAttribute{
			ElementType: et, Required: a.Required, Optional: a.Optional,
			Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	case "map":
		return dsschema.MapAttribute{
			ElementType: et, Required: a.Required, Optional: a.Optional,
			Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	default: // list
		return dsschema.ListAttribute{
			ElementType: et, Required: a.Required, Optional: a.Optional,
			Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	}
}

// dsNestedAttrMap builds a nested attribute map for data source object schemas.
// All nested attributes are forced to Computed — they live inside a Computed
// parent and are always populated from the API response, never set by the user.
func dsNestedAttrMap(attrs []AttributeSpec) map[string]dsschema.Attribute {
	out := make(map[string]dsschema.Attribute, len(attrs))
	for _, na := range attrs {
		adj := na
		adj.Required = false
		adj.Optional = false
		adj.Computed = true
		out[na.Name] = dsAttrSchema(adj)
	}
	return out
}

func dsObjectSchema(a AttributeSpec, info typeInfo) dsschema.Attribute {
	nested := dsNestedAttrMap(a.Attributes)
	switch info.coll {
	case "list":
		return dsschema.ListNestedAttribute{
			NestedObject: dsschema.NestedAttributeObject{Attributes: nested},
			Required:     a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	case "set":
		return dsschema.SetNestedAttribute{
			NestedObject: dsschema.NestedAttributeObject{Attributes: nested},
			Required:     a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	case "map":
		return dsschema.MapNestedAttribute{
			NestedObject: dsschema.NestedAttributeObject{Attributes: nested},
			Required:     a.Required, Optional: a.Optional, Computed: a.Computed,
			Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	default: // single object
		return dsschema.SingleNestedAttribute{
			Attributes: nested, Required: a.Required, Optional: a.Optional,
			Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description, DeprecationMessage: a.Deprecated,
		}
	}
}
