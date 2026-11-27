package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/dg-i/terraform-provider-rcodezero/internal/rcode0api"
)

// Shaped like real API responses (2026-11-19), with names and IPs replaced.
const (
	slaveZoneJSON = `{"id":1,"domain":"example.at","type":"SLAVE","masters":["192.0.2.1"],"serial":null,` +
		`"created":"2026-11-19T13:08:01Z","last_check":null,"dnssec_status":"Unsigned","dnssec_status_detail":"Unsigned",` +
		`"serialincoming":null,"zone_disabled":false,"nsset":["sec1.rcode0.eu","sec2.rcode0.net"],` +
		`"outbound_xfr_host":{"ips":["198.51.100.10"],"port":53},"cds_cdnskey_publish":false}`
	masterZoneJSON = `{"id":2,"domain":"example.at","type":"MASTER","masters":[],"serial":2026111901,` +
		`"created":"2026-11-19T13:08:01Z","last_check":null,"dnssec_status":"Unsigned","dnssec_status_detail":"Unsigned",` +
		`"zone_disabled":true,"nsset":["sec1.rcode0.eu","sec2.rcode0.net"],` +
		`"outbound_xfr_host":{"ips":["198.51.100.10"],"port":53},"cds_cdnskey_publish":false,"serial_auto_increment":true}`
)

// Given an API response and the masters from config/state, when read() copies
// the zone into the model, then the model is normalised to what the user writes.
func TestRead(t *testing.T) {
	emptySet := types.SetValueMust(types.StringType, nil)
	nullSet := types.SetNull(types.StringType)

	tests := []struct {
		name              string
		given             apiResponse
		givenPriorMasters types.Set
		then              func(t *testing.T, m zoneResourceModel)
		thenNotFound      bool
		thenErr           string
	}{
		{
			name:              "slave zone",
			given:             apiResponse{200, jsonType, slaveZoneJSON},
			givenPriorMasters: nullSet,
			then: func(t *testing.T, m zoneResourceModel) {
				expect(t, "id", m.ID, types.StringValue("example.at"))
				expect(t, "type", m.Type, types.StringValue("slave")) // API sends SLAVE
				expect(t, "masters", m.Masters, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("192.0.2.1")}))
				expect(t, "disabled", m.Disabled, types.BoolValue(false))
				expect(t, "serial_auto_increment", m.SerialAutoIncrement, types.BoolNull()) // omitted for slave zones
				if len(m.NSSet.Elements()) != 2 {
					t.Errorf("nsset = %s, want 2 entries", m.NSSet)
				}
			},
		},
		{
			name:              "master zone, masters not configured",
			given:             apiResponse{200, jsonType, masterZoneJSON},
			givenPriorMasters: nullSet,
			then: func(t *testing.T, m zoneResourceModel) {
				expect(t, "type", m.Type, types.StringValue("master"))
				expect(t, "masters", m.Masters, nullSet)
				expect(t, "disabled", m.Disabled, types.BoolValue(true))
				expect(t, "serial_auto_increment", m.SerialAutoIncrement, types.BoolValue(true))
			},
		},
		{
			name:              "master zone, masters = [] configured",
			given:             apiResponse{200, jsonType, masterZoneJSON},
			givenPriorMasters: emptySet,
			then: func(t *testing.T, m zoneResourceModel) {
				// Must stay [] and not become null, or tofu reports an inconsistent result.
				expect(t, "masters", m.Masters, emptySet)
			},
		},
		{
			name:              "not found",
			given:             apiResponse{404, jsonType, `{"status":"failed","message":"Zone not found"}`},
			givenPriorMasters: nullSet,
			thenNotFound:      true, // Read then removes the resource from state
		},
		{
			name:              "HTML instead of JSON",
			given:             apiResponse{200, "text/html", "<html>login</html>"},
			givenPriorMasters: nullSet,
			thenErr:           `HTTP 200 without JSON body (Content-Type "text/html")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/zones/example.at" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", tt.given.contentType)
				w.WriteHeader(tt.given.status)
				_, _ = io.WriteString(w, tt.given.body)
			}))
			t.Cleanup(srv.Close)
			client, err := rcode0api.NewClientWithResponses(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			m := zoneResourceModel{Domain: types.StringValue("example.at"), Masters: tt.givenPriorMasters}

			// when
			found, err := (&zoneResource{client: client}).read(t.Context(), &m)

			// then
			if tt.thenErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.thenErr) {
					t.Fatalf("err = %v, want error containing %q", err, tt.thenErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found == tt.thenNotFound {
				t.Fatalf("found = %v, want %v", found, !tt.thenNotFound)
			}
			if tt.then != nil {
				tt.then(t, m)
			}
		})
	}
}

// Given an import ID in non-canonical form, when importing, then domain and id
// are stored lower case without trailing dot.
func TestImportStateNormalizesDomain(t *testing.T) {
	// given
	ctx := t.Context()
	r := &zoneResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	resp := resource.ImportStateResponse{State: tfsdk.State{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
	}}

	// when
	r.ImportState(ctx, resource.ImportStateRequest{ID: "Example.AT."}, &resp)

	// then
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error: %v", resp.Diagnostics)
	}
	for _, name := range []string{"domain", "id"} {
		var got types.String
		resp.State.GetAttribute(ctx, path.Root(name), &got)
		expect(t, name, got, types.StringValue("example.at"))
	}
}

func expect(t *testing.T, name string, got, want attr.Value) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
