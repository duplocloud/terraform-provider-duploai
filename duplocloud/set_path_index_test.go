package duplocloud

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Array-index request paths exist because AWS-shaped bodies wrap a single value
// in a list: one CIDR must be sent as ipv4Ranges[0].cidrIp, while the read side
// returns it flat as cidrIpv4. Without index support the spec would have to
// expose the API's list shape in HCL.
func TestSetPath_ArrayIndexBuildsListOfObjects(t *testing.T) {
	body := map[string]any{}
	setPath(body, []string{"ipv4Ranges[0]", "cidrIp"}, "10.0.0.0/8")
	setPath(body, []string{"ipv4Ranges[0]", "description"}, "office")
	setPath(body, []string{"ipProtocol"}, "tcp")

	got, _ := json.Marshal(body)
	want := `{"ipProtocol":"tcp","ipv4Ranges":[{"cidrIp":"10.0.0.0/8","description":"office"}]}`
	if string(got) != want {
		t.Errorf("body =\n  %s\nwant\n  %s", got, want)
	}
}

// A terminal index writes the value into the slot itself rather than into an
// object, and gaps are filled so index N always exists.
func TestSetPath_TerminalIndexAndGaps(t *testing.T) {
	body := map[string]any{}
	setPath(body, []string{"values[2]"}, "third")

	arr, ok := body["values"].([]any)
	if !ok {
		t.Fatalf("values is %T, want []any", body["values"])
	}
	if len(arr) != 3 || arr[2] != "third" || arr[0] != nil || arr[1] != nil {
		t.Errorf("values = %#v, want [nil nil third]", arr)
	}
}

// A path with no index must behave exactly as before — this is the path every
// other resource in the provider takes.
func TestSetPath_PlainPathsUnchanged(t *testing.T) {
	body := map[string]any{}
	setPath(body, []string{"spec", "provisioner", "type"}, "Cli")
	got, _ := json.Marshal(body)
	if want := `{"spec":{"provisioner":{"type":"Cli"}}}`; string(got) != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// A segment that merely contains brackets, or a non-numeric index, is treated as
// a plain key rather than silently mis-parsed into an array.
func TestSetPath_NonIndexSegmentsAreKeys(t *testing.T) {
	for _, seg := range []string{"weird[abc]", "[0]trailing", "no-brackets"} {
		body := map[string]any{}
		setPath(body, []string{seg, "leaf"}, "v")
		if _, ok := body[seg].(map[string]any); !ok {
			t.Errorf("segment %q was not treated as a plain object key: %#v", seg, body)
		}
	}
}

// End-to-end through the request builder: the spec's requestPath is what routes a
// flat HCL attribute into the AWS list shape.
func TestSetPath_RequestPathFromSpec(t *testing.T) {
	spec := ResourceSpec{
		Name: "sgr", IDPath: "securityGroupRuleId",
		Endpoint: EndpointSpec{UriBase: "/rules"},
		Attributes: []AttributeSpec{
			{Name: "ip_protocol", Type: "string", Required: true, APIPath: "ipProtocol"},
			{Name: "cidr_ipv4", Type: "string", Optional: true,
				RequestPath: "ipv4Ranges[0].cidrIp", ResponsePath: "cidrIpv4"},
		},
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("spec rejected: %v", err)
	}
	r := &dynamicResource{spec: spec}

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"id":          tftypes.String,
		"ip_protocol": tftypes.String,
		"cidr_ipv4":   tftypes.String,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":          tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"ip_protocol": tftypes.NewValue(tftypes.String, "tcp"),
		"cidr_ipv4":   tftypes.NewValue(tftypes.String, "203.0.113.0/24"),
	})

	body := r.bodyFromRaw(raw, "create", nil)
	ranges, ok := body["ipv4Ranges"].([]any)
	if !ok || len(ranges) != 1 {
		t.Fatalf("ipv4Ranges = %#v, want a one-element list", body["ipv4Ranges"])
	}
	elem, ok := ranges[0].(map[string]any)
	if !ok || elem["cidrIp"] != "203.0.113.0/24" {
		t.Errorf("ipv4Ranges[0] = %#v, want {cidrIp: 203.0.113.0/24}", ranges[0])
	}
}
