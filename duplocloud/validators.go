package duplocloud

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type noWhitespaceValidator struct{}

func (v noWhitespaceValidator) Description(_ context.Context) string {
	return "Value must not contain whitespace."
}

func (v noWhitespaceValidator) MarkdownDescription(_ context.Context) string {
	return "Value must not contain whitespace."
}

func (v noWhitespaceValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, c := range req.ConfigValue.ValueString() {
		if c == ' ' || c == '\t' || c == '\n' {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Whitespace not allowed",
				"The value must not contain whitespace characters.",
			)
			return
		}
	}
}

// NoWhitespace returns a validator that rejects strings containing whitespace.
func NoWhitespace() validator.String {
	return noWhitespaceValidator{}
}

// reservedKeyPrefixValidator warns about map keys under a prefix the backend
// strips (AttributeSpec.ReservedKeyPrefixes). A warning, not an error: such a
// config applied before and must keep planning.
type reservedKeyPrefixValidator struct{ prefixes []string }

func (v reservedKeyPrefixValidator) Description(_ context.Context) string {
	return fmt.Sprintf("Keys starting with %s are reserved by the platform and are not applied.", strings.Join(v.prefixes, ", "))
}

func (v reservedKeyPrefixValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v reservedKeyPrefixValidator) ValidateMap(_ context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var keys []string
	for k := range req.ConfigValue.Elements() {
		for _, p := range v.prefixes {
			if strings.HasPrefix(k, p) {
				keys = append(keys, k)
				break
			}
		}
	}
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)
	resp.Diagnostics.AddAttributeWarning(req.Path, "Reserved key is not applied",
		fmt.Sprintf("%s: keys starting with %s are reserved by the platform, which removes them from the object, "+
			"so they never reach state and every plan shows them as an update. Remove them from the configuration.",
			strings.Join(keys, ", "), strings.Join(v.prefixes, ", ")))
}
