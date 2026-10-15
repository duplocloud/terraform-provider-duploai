package duplocloud

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
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
