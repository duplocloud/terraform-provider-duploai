package duplocloud

import (
	"context"

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
