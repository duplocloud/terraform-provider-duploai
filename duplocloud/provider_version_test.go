package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// The provider reports its release version to Terraform, and the User-Agent
// names both the provider and Terraform versions.
func TestProviderVersionAndUserAgent(t *testing.T) {
	var resp provider.MetadataResponse
	NewVersioned("0.7.1")().Metadata(context.Background(), provider.MetadataRequest{}, &resp)
	if resp.Version != "0.7.1" || resp.TypeName != "duploai" {
		t.Errorf("Metadata = %+v, want version 0.7.1 and type duploai", resp)
	}
	New().Metadata(context.Background(), provider.MetadataRequest{}, &resp)
	if resp.Version != "dev" {
		t.Errorf("New() version = %q, want dev", resp.Version)
	}
	cases := map[[2]string]string{
		{"0.7.1", "1.9.8"}: "terraform-provider-duploai/0.7.1 (+https://registry.terraform.io/providers/duplocloud/duploai) Terraform/1.9.8",
		{"", ""}:           "terraform-provider-duploai/dev (+https://registry.terraform.io/providers/duplocloud/duploai)",
	}
	for in, want := range cases {
		if got := userAgent(in[0], in[1]); got != want {
			t.Errorf("userAgent(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// An unknown duplo_host/duplo_token (set from a value known only after apply)
// keeps its old outcome — the env var still wins when set, and it is still an
// error without one — but the error now says the value is unknown, not missing.
func TestConfigureUnknownHostOrToken(t *testing.T) {
	ctx := context.Background()
	p := New()
	var sr provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sr)
	typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	cfg := func(host, token tftypes.Value) tfsdk.Config {
		vals := map[string]tftypes.Value{}
		for n, at := range typ.AttributeTypes {
			vals[n] = tftypes.NewValue(at, nil)
		}
		vals["duplo_host"], vals["duplo_token"] = host, token
		return tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, vals)}
	}
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	null := tftypes.NewValue(tftypes.String, nil)
	for _, k := range []string{"DUPLO_HOST", "duplo_host", "DUPLO_TOKEN", "duplo_token", "SSL_NO_VERIFY", "ssl_no_verify", "HTTP_TIMEOUT", "http_timeout"} {
		t.Setenv(k, "")
	}
	run := func(c tfsdk.Config) provider.ConfigureResponse {
		var resp provider.ConfigureResponse
		p.Configure(ctx, provider.ConfigureRequest{Config: c}, &resp)
		return resp
	}

	r := run(cfg(unknown, str("tok")))
	if !r.Diagnostics.HasError() || r.Diagnostics.Errors()[0].Summary() != "Unknown duplo_host" {
		t.Errorf("unknown host, no env: want the 'Unknown duplo_host' error, got %v", r.Diagnostics)
	}
	t.Setenv("DUPLO_HOST", "http://example.invalid")
	if r := run(cfg(unknown, str("tok"))); r.Diagnostics.HasError() || r.ResourceData == nil {
		t.Errorf("unknown host with DUPLO_HOST set: must still configure from the env var, got %v", r.Diagnostics)
	}
	t.Setenv("DUPLO_HOST", "")
	if r := run(cfg(null, str("tok"))); !r.Diagnostics.HasError() || r.Diagnostics.Errors()[0].Summary() != "Missing duplo_host" {
		t.Errorf("unset host: want the unchanged 'Missing duplo_host' error, got %v", r.Diagnostics)
	}
	if r := run(cfg(str("http://example.invalid"), unknown)); !r.Diagnostics.HasError() || r.Diagnostics.Errors()[0].Summary() != "Unknown duplo_token" {
		t.Errorf("unknown token: want the 'Unknown duplo_token' error, got %v", r.Diagnostics)
	}
	if r := run(cfg(str("http://example.invalid"), str("tok"))); r.Diagnostics.HasError() {
		t.Errorf("known host and token: want no error, got %v", r.Diagnostics)
	}
}
