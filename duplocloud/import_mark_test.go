package duplocloud

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Import by identity must set the same private-state import mark as import by
// ID: without it, native_host's importAdoptsConfig attributes (instance_type,
// ebs_optimized — never returned by the API, so null after import) plan their
// defaults as a forceNew change and replace the host just imported.
func TestImportResourceState_MarksImportedByIDAndIdentity(t *testing.T) {
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatal(err)
	}
	const name = "duploai_native_host"
	ids, err := server.GetResourceIdentitySchemas(ctx, &tfprotov6.GetResourceIdentitySchemasRequest{})
	if err != nil {
		t.Fatal(err)
	}
	idSchema, ok := ids.IdentitySchemas[name]
	if !ok {
		t.Fatalf("%s has no identity schema", name)
	}
	idType := idSchema.ValueType().(tftypes.Object)
	idVals := map[string]tftypes.Value{}
	for n := range idType.AttributeTypes {
		idVals[n] = tftypes.NewValue(tftypes.String, "seg-"+n)
	}
	idVals["id"] = tftypes.NewValue(tftypes.String, "obj1")
	identity, err := tfprotov6.NewDynamicValue(idType, tftypes.NewValue(idType, idVals))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]*tfprotov6.ImportResourceStateRequest{
		"by id":       {TypeName: name, ID: "ws1/obj1"},
		"by identity": {TypeName: name, Identity: &tfprotov6.ResourceIdentityData{IdentityData: &identity}},
	}
	for label, req := range cases {
		resp, err := server.ImportResourceState(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		for _, d := range resp.Diagnostics {
			if d.Severity == tfprotov6.DiagnosticSeverityError {
				t.Fatalf("%s: %s: %s", label, d.Summary, d.Detail)
			}
		}
		if len(resp.ImportedResources) != 1 {
			t.Fatalf("%s: %d imported resources", label, len(resp.ImportedResources))
		}
		if p := string(resp.ImportedResources[0].Private); !strings.Contains(p, importedPrivateKey) {
			t.Errorf("%s: private state %q lacks the %q mark", label, p, importedPrivateKey)
		}
	}
}
