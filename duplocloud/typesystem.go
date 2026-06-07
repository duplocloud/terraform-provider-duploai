package duplocloud

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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
		o := schema.StringAttribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.Default != nil {
			var s string
			_ = json.Unmarshal(*a.Default, &s)
			o.Default = stringdefault.StaticString(s)
		}
		if len(a.OneOf) > 0 {
			o.Validators = []validator.String{stringvalidator.OneOf(a.OneOf...)}
		}
		var pm []planmodifier.String
		if a.Computed {
			pm = append(pm, stringplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, stringplanmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
		return o
	case "bool":
		o := schema.BoolAttribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.Default != nil {
			var b bool
			_ = json.Unmarshal(*a.Default, &b)
			o.Default = booldefault.StaticBool(b)
		}
		var pm []planmodifier.Bool
		if a.Computed {
			pm = append(pm, boolplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, boolplanmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
		return o
	case "int":
		o := schema.Int64Attribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.Default != nil {
			var i int64
			_ = json.Unmarshal(*a.Default, &i)
			o.Default = int64default.StaticInt64(i)
		}
		var pm []planmodifier.Int64
		if a.Computed {
			pm = append(pm, int64planmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, int64planmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
		return o
	default: // number
		o := schema.Float64Attribute{Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.Default != nil {
			var f float64
			_ = json.Unmarshal(*a.Default, &f)
			o.Default = float64default.StaticFloat64(f)
		}
		var pm []planmodifier.Float64
		if a.Computed {
			pm = append(pm, float64planmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, float64planmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
		return o
	}
}

func primitiveCollectionSchema(a AttributeSpec, info typeInfo) schema.Attribute {
	et := primitiveAttrType(info.elem)
	switch info.coll {
	case "set":
		o := schema.SetAttribute{ElementType: et, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		var pm []planmodifier.Set
		if a.Computed {
			pm = append(pm, setplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, setplanmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
		return o
	case "map":
		o := schema.MapAttribute{ElementType: et, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		var pm []planmodifier.Map
		if a.Computed {
			pm = append(pm, mapplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, mapplanmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
		return o
	default: // list
		o := schema.ListAttribute{ElementType: et, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		var pm []planmodifier.List
		if a.Computed {
			pm = append(pm, listplanmodifier.UseStateForUnknown())
		}
		if a.ForceNew {
			pm = append(pm, listplanmodifier.RequiresReplace())
		}
		o.PlanModifiers = pm
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
		o := schema.ListNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: nested}, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.ForceNew {
			o.PlanModifiers = []planmodifier.List{listplanmodifier.RequiresReplace()}
		}
		return o
	case "set":
		o := schema.SetNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: nested}, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.ForceNew {
			o.PlanModifiers = []planmodifier.Set{setplanmodifier.RequiresReplace()}
		}
		return o
	case "map":
		o := schema.MapNestedAttribute{NestedObject: schema.NestedAttributeObject{Attributes: nested}, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.ForceNew {
			o.PlanModifiers = []planmodifier.Map{mapplanmodifier.RequiresReplace()}
		}
		return o
	default: // single object
		o := schema.SingleNestedAttribute{Attributes: nested, Required: a.Required, Optional: a.Optional, Computed: a.Computed, Sensitive: a.Sensitive, Description: a.Description}
		if a.ForceNew {
			o.PlanModifiers = []planmodifier.Object{objectplanmodifier.RequiresReplace()}
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
func attrFromResponse(a AttributeSpec, t tftypes.Type, data any) tftypes.Value {
	if data == nil {
		return tftypes.NewValue(t, nil)
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" {
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
