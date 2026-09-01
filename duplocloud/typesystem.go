package duplocloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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

// stringValidators builds the plan-time validators for a string attribute, so a
// value the API would reject fails before any call is made rather than as a 400
// mid-apply. Shared by the resource and data source schemas — a constraint that
// held on one but not the other would be a trap.
//
// Returns nil when nothing is constrained, which is the framework's "no
// validators" and keeps the common case allocation-free.
func stringValidators(a AttributeSpec) []validator.String {
	var out []validator.String
	if len(a.OneOf) > 0 {
		out = append(out, stringvalidator.OneOf(a.OneOf...))
	}
	if a.MaxLength > 0 {
		out = append(out, stringvalidator.LengthAtMost(a.MaxLength))
	}
	if a.Pattern != "" {
		// validate() compiles this at spec load, so a spec carrying a bad pattern
		// never reaches here; MustCompile cannot fire on a loaded spec.
		msg := a.PatternDescription
		if msg == "" {
			msg = "must match " + a.Pattern
		}
		out = append(out, stringvalidator.RegexMatches(regexp.MustCompile(a.Pattern), msg))
	}
	return out
}

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
		o.Validators = stringValidators(a)
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
		if a.ImmutableOnceTrue {
			o.PlanModifiers = append(o.PlanModifiers, immutableOnceTrueModifier{attr: a.Name})
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
	return a.Computed && (a.Optional || a.ForceNew || a.preserveTarget || a.Stable || a.SendFromState)
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
		if a.OrderByKey != "" {
			// Sort the planned config by the key so it matches the read side's
			// canonical order — must run after UseStateForUnknown resolves the value.
			o.PlanModifiers = append(o.PlanModifiers, orderByKeyModifier{key: a.OrderByKey})
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

// immutableOnceTrueModifier fails the plan when a one-way boolean is turned back
// off. See AttributeSpec.ImmutableOnceTrue for why this errors instead of
// quietly holding the old value.
type immutableOnceTrueModifier struct{ attr string }

func (m immutableOnceTrueModifier) Description(_ context.Context) string {
	return "cannot be set back to false once true; the resource must be recreated"
}

func (m immutableOnceTrueModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m immutableOnceTrueModifier) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	// Null state = create. Unknown either side = nothing decided yet. A NULL plan
	// means the attribute is not set at all, which is not the same as asking for
	// false — but ValueBool() reports both as false, so it has to be excluded
	// explicitly or an unset attribute would look like a request to disable.
	// (Unreachable on an attribute with a default, and the framework skips plan
	// modifiers on destroy, but the flag is general.)
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() ||
		req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if !req.StateValue.ValueBool() || req.PlanValue.ValueBool() {
		return // staying off, turning on, or already on — all fine
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Cannot disable "+m.attr,
		"This setting is one-way: once enabled the cloud provider does not allow turning it "+
			"off again, so the change would fail during apply.\n\n"+
			"Set it back to true, destroy and recreate the resource, or keep the config as-is "+
			"and add lifecycle { ignore_changes = ["+m.attr+"] } to stop Terraform planning "+
			"the change.",
	)
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
		g := tftypesToGo(v)
		if a.StringBool {
			// The API holds this field in a string-valued container (e.g. a
			// Dictionary<string,string> metadata map), so send "true"/"false"
			// rather than a JSON boolean it cannot deserialize.
			if b, ok := g.(bool); ok {
				return strconv.FormatBool(b), true
			}
		}
		return g, true
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

// normalizeVersionMinor truncates a version string to its major.minor components
// (first two dot-separated parts), so a minor-precision config ("1.35") matches a
// backend that resolves it to a patch version ("1.35.6"). Values with two or
// fewer components are returned unchanged.
func normalizeVersionMinor(s string) string {
	parts := strings.Split(s, ".")
	if len(parts) <= 2 {
		return s
	}
	return strings.Join(parts[:2], ".")
}

func attrFromResponse(a AttributeSpec, t tftypes.Type, data any) tftypes.Value {
	if data == nil {
		return tftypes.NewValue(t, nil)
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" {
		if a.StringBool {
			// Wire form is the string "true"/"false" (see AttributeSpec.StringBool).
			// Only an explicit "true" is true, matching the platform's own reading of
			// these keys; a real bool is tolerated in case the API ever sends one.
			switch s := data.(type) {
			case string:
				data = strings.EqualFold(s, "true")
			case bool:
				data = s
			}
		}
		if a.NormalizeCsvOrder {
			if s, ok := data.(string); ok {
				data = normalizeCsvOrder(s)
			}
		}
		if a.NormalizeVersion {
			if s, ok := data.(string); ok {
				data = normalizeVersionMinor(s)
			}
		}
		// Flatten before filtering: mapValuePath turns the wrapped entries into
		// plain strings, filterResponseKeys then drops whole keys from that.
		if a.MapValuePath != "" {
			data = flattenMapValues(data, a.MapValuePath, a.MapDropWhenTrue)
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
		if a.OrderByKey != "" {
			arr = sortObjectsByKey(arr, a.orderKeyResponseKey())
		}
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
	// Known at this level but with unknown descendants: nested objects (single,
	// or in a list/map) merge per field so a computed child is filled from the
	// response without discarding its configured siblings. Anything else —
	// primitive collections, set(object), a shape mismatch — is rebuilt wholesale.
	info, _ := parseType(a.Type)
	if info.elem != "object" {
		return attrFromResponse(a, t, respData)
	}
	switch info.coll {
	case "":
		return mergeObjectUnknown(a.Attributes, t, plan, respData)
	case "list":
		lt, ok := t.(tftypes.List)
		if !ok {
			return attrFromResponse(a, t, respData)
		}
		var cur []tftypes.Value
		if err := plan.As(&cur); err != nil {
			return attrFromResponse(a, t, respData)
		}
		arr := toAnySlice(respData)
		if a.OrderByKey != "" {
			arr = sortObjectsByKey(arr, a.orderKeyResponseKey())
		}
		// Elements are paired by index, so a response of a different length
		// cannot be aligned with the plan — fall back to the response.
		if len(arr) != len(cur) {
			return attrFromResponse(a, t, respData)
		}
		out := make([]tftypes.Value, 0, len(cur))
		for i, e := range cur {
			out = append(out, mergeObjectUnknown(a.Attributes, lt.ElementType, e, arr[i]))
		}
		return tftypes.NewValue(t, out)
	case "map":
		mt, ok := t.(tftypes.Map)
		if !ok {
			return attrFromResponse(a, t, respData)
		}
		var cur map[string]tftypes.Value
		if err := plan.As(&cur); err != nil {
			return attrFromResponse(a, t, respData)
		}
		dm := toAnyMap(respData)
		if len(dm) != len(cur) {
			return attrFromResponse(a, t, respData)
		}
		out := make(map[string]tftypes.Value, len(cur))
		for k, e := range cur {
			re, present := dm[k]
			if !present {
				return attrFromResponse(a, t, respData)
			}
			out[k] = mergeObjectUnknown(a.Attributes, mt.ElementType, e, re)
		}
		return tftypes.NewValue(t, out)
	default: // set — element order is not meaningful, so plan and response
		// elements cannot be paired reliably.
		return attrFromResponse(a, t, respData)
	}
}

// mergeObjectUnknown merges one object value field by field: each configured
// (known) field is kept and each unknown one is filled from the response.
func mergeObjectUnknown(attrs []AttributeSpec, t tftypes.Type, plan tftypes.Value, respData any) tftypes.Value {
	if plan.IsFullyKnown() {
		return plan
	}
	ot, isObj := t.(tftypes.Object)
	if !isObj {
		return goToTftypesValue(t, respData)
	}
	var cur map[string]tftypes.Value
	if err := plan.As(&cur); err != nil {
		return objectFromResponse(attrs, t, respData)
	}
	dm, _ := respData.(map[string]any)
	out := make(map[string]tftypes.Value, len(ot.AttributeTypes))
	for _, na := range attrs {
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

// hasPreserveOnEmptyResponse reports whether an attribute, or any attribute
// nested below it, is marked PreserveOnEmptyResponse. Used to skip the restore
// walk entirely for the (overwhelming majority of) attributes that do not need it.
func hasPreserveOnEmptyResponse(a AttributeSpec) bool {
	if a.PreserveOnEmptyResponse {
		return true
	}
	for _, na := range a.Attributes {
		if hasPreserveOnEmptyResponse(na) {
			return true
		}
	}
	return false
}

// restorePreservedValues walks a freshly built state value alongside the value
// it replaces (the plan on create/update, the prior state on refresh) and, at
// every leaf marked PreserveOnEmptyResponse that the API returned as null or
// empty, keeps the prior value instead. This is what lets a write-only secret —
// redacted by the backend on every read — stay in state instead of being
// blanked. See AttributeSpec.PreserveOnEmptyResponse.
func restorePreservedValues(a AttributeSpec, prior, next tftypes.Value) tftypes.Value {
	if !hasPreserveOnEmptyResponse(a) {
		return next
	}
	if a.PreserveOnEmptyResponse {
		if isEmptyStateValue(next) && prior.IsKnown() && !prior.IsNull() && prior.Type().Is(next.Type()) {
			return prior
		}
		return next
	}
	info, _ := parseType(a.Type)
	if info.elem != "object" || next.IsNull() || !next.IsKnown() {
		return next
	}
	switch info.coll {
	case "":
		return restoreObjectPreserved(a.Attributes, prior, next)
	case "list":
		var nextElems, priorElems []tftypes.Value
		if err := next.As(&nextElems); err != nil {
			return next
		}
		if prior.IsKnown() && !prior.IsNull() {
			_ = prior.As(&priorElems)
		}
		out := make([]tftypes.Value, 0, len(nextElems))
		for i, e := range nextElems {
			out = append(out, restoreObjectPreserved(a.Attributes, elemAt(priorElems, i, e.Type()), e))
		}
		return tftypes.NewValue(next.Type(), out)
	case "map":
		var nextElems, priorElems map[string]tftypes.Value
		if err := next.As(&nextElems); err != nil {
			return next
		}
		if prior.IsKnown() && !prior.IsNull() {
			_ = prior.As(&priorElems)
		}
		out := make(map[string]tftypes.Value, len(nextElems))
		for k, e := range nextElems {
			p, ok := priorElems[k]
			if !ok {
				p = tftypes.NewValue(e.Type(), nil)
			}
			out[k] = restoreObjectPreserved(a.Attributes, p, e)
		}
		return tftypes.NewValue(next.Type(), out)
	default: // set — elements cannot be paired positionally.
		return next
	}
}

// restoreObjectPreserved applies restorePreservedValues to each field of one
// object value, pairing it with the same field of the prior object.
func restoreObjectPreserved(attrs []AttributeSpec, prior, next tftypes.Value) tftypes.Value {
	if next.IsNull() || !next.IsKnown() {
		return next
	}
	var nm map[string]tftypes.Value
	if err := next.As(&nm); err != nil {
		return next
	}
	var pm map[string]tftypes.Value
	if prior.IsKnown() && !prior.IsNull() {
		_ = prior.As(&pm)
	}
	out := make(map[string]tftypes.Value, len(nm))
	for k, v := range nm {
		out[k] = v
	}
	for _, na := range attrs {
		child, ok := nm[na.Name]
		if !ok {
			continue
		}
		p, ok := pm[na.Name]
		if !ok {
			p = tftypes.NewValue(child.Type(), nil)
		}
		out[na.Name] = restorePreservedValues(na, p, child)
	}
	return tftypes.NewValue(next.Type(), out)
}

// elemAt returns the i-th element of elems, or a typed null when the prior
// collection is shorter (or absent).
func elemAt(elems []tftypes.Value, i int, t tftypes.Type) tftypes.Value {
	if i < len(elems) {
		return elems[i]
	}
	return tftypes.NewValue(t, nil)
}

// isEmptyStateValue reports whether a value carries nothing worth storing —
// null, unknown, or the empty string a redacting backend returns in place of a
// secret.
func isEmptyStateValue(v tftypes.Value) bool {
	if v.IsNull() || !v.IsKnown() {
		return true
	}
	if v.Type().Is(tftypes.String) {
		var s string
		if err := v.As(&s); err == nil {
			return s == ""
		}
	}
	return false
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
		o.Validators = stringValidators(a)
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
