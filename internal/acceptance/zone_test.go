package acceptance

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/dg-i/terraform-provider-rcodezero/internal/mockserver"
)

const zoneAddr = "rcodezero_zone.test"

// Given a slave zone, when it goes through its lifecycle step by step, then
// every change but a rename is applied in place and the API matches the state.
func TestZoneResource_lifecycle(t *testing.T) {
	// given
	mock, provider, _ := startMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		CheckDestroy:             checkZoneGone(mock, "example.at"),
		Steps: []resource.TestStep{
			{
				// when a slave zone is created
				Config: zoneConfig(provider, `
  domain  = "example.at"
  type    = "slave"
  masters = ["192.0.2.1", "2001:db8::1"]`),
				// then the state shows it as the API returns it
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttr("id", knownvalue.StringExact("example.at")),
					expectAttr("type", knownvalue.StringExact("slave")),
					expectAttr("masters", knownvalue.SetExact([]knownvalue.Check{
						knownvalue.StringExact("192.0.2.1"), knownvalue.StringExact("2001:db8::1"),
					})),
					expectAttr("disabled", knownvalue.Bool(false)),
					expectAttr("serial_auto_increment", knownvalue.Null()), // omitted by the API for slave zones
					expectAttr("nsset", knownvalue.ListSizeExact(2)),
				},
			},
			{
				// when the masters change
				Config: zoneConfig(provider, `
  domain  = "example.at"
  type    = "slave"
  masters = ["192.0.2.2"]`),
				// then the zone is updated in place
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttr("masters", knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("192.0.2.2")})),
				},
			},
			{
				// when the zone is imported, then the imported state equals the current one
				ResourceName:      zoneAddr,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// when it becomes a master zone and is disabled in the same apply
				// (zone_disabled only works on master zones, so PUT must come before PATCH)
				Config: zoneConfig(provider, `
  domain   = "example.at"
  type     = "master"
  disabled = true`),
				// then both changes are applied in place
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttr("type", knownvalue.StringExact("master")),
					expectAttr("masters", knownvalue.Null()),
					expectAttr("disabled", knownvalue.Bool(true)),
					expectAttr("serial_auto_increment", knownvalue.Bool(true)), // API default for master zones
				},
				Check: checkZone(mock, "example.at", func(z mockserver.ZoneDetails) error {
					if z.Type != mockserver.TypeWithMemberMASTER || len(z.Masters) != 0 || !z.ZoneDisabled {
						return fmt.Errorf("unexpected zone in API: type=%s masters=%v disabled=%v", z.Type, z.Masters, z.ZoneDisabled)
					}
					return nil
				}),
			},
			{
				// when the disabled master zone becomes a slave zone again
				// (PATCH to re-enable must come before PUT)
				Config: zoneConfig(provider, `
  domain  = "example.at"
  type    = "slave"
  masters = ["192.0.2.1"]`),
				// then it is enabled and serial_auto_increment is gone again
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttr("type", knownvalue.StringExact("slave")),
					expectAttr("disabled", knownvalue.Bool(false)),
					expectAttr("serial_auto_increment", knownvalue.Null()),
				},
			},
			{
				// when the zone is renamed
				Config: zoneConfig(provider, `
  domain = "example2.at"
  type   = "master"`),
				// then the old zone is replaced by the new one
				ConfigPlanChecks: expectAction(plancheck.ResourceActionDestroyBeforeCreate),
				Check: resource.ComposeTestCheckFunc(
					checkZoneGone(mock, "example.at"),
					checkZoneExists(mock, "example2.at"),
				),
			},
		},
	})
}

// Given a zone that is deleted outside of tofu, when tofu refreshes, then it
// plans to create the zone again.
func TestZoneResource_deletedOutOfBand(t *testing.T) {
	// given
	mock, provider, _ := startMock(t)
	config := zoneConfig(provider, `
  domain = "gone.at"
  type   = "master"`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				// when the zone disappears from the API right after apply
				Check: func(*terraform.State) error {
					mock.RemoveZone("gone.at")
					return nil
				},
				// then the refresh after apply notices it and plans to recreate it
				ExpectNonEmptyPlan: true,
			},
			{
				// when applying again, then the zone exists again
				Config: config,
				Check:  checkZoneExists(mock, "gone.at"),
			},
		},
	})
}

// Given an invalid zone config, when planning, then tofu fails with a clear
// message before any API call.
func TestZoneResource_validation(t *testing.T) {
	// given
	_, provider, _ := startMock(t)

	tests := []struct {
		name    string
		given   string
		thenErr string
	}{
		{"slave without masters", `
  domain = "example.at"
  type   = "slave"`, `Slave zones need at least one master`},
		{"master with masters", `
  domain  = "example.at"
  type    = "master"
  masters = ["192.0.2.1"]`, `Master zones must not set masters`},
		{"unknown type", `
  domain = "example.at"
  type   = "consumer"`, `value must be one of`},
		{"slave with serial_auto_increment", `
  domain                = "example.at"
  type                  = "slave"
  masters               = ["192.0.2.1"]
  serial_auto_increment = false`, `serial_auto_increment only applies to master zones`},
		{"disabled slave", `
  domain   = "example.at"
  type     = "slave"
  masters  = ["192.0.2.1"]
  disabled = true`, `disabled only applies to master zones`},
		{"non-canonical domain", `
  domain = "Example.at."
  type   = "master"`, `must be a lower-case zone name without trailing dot`},
	}

	steps := make([]resource.TestStep, 0, len(tests))
	for _, tt := range tests {
		steps = append(steps, resource.TestStep{
			// when
			Config: zoneConfig(provider, tt.given),
			// then
			ExpectError: regexp.MustCompile(tt.thenErr),
		})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testProviderFactories, Steps: steps})
}

// Given a master zone configured with masters = [], when it is applied, then
// masters stays an empty set instead of turning into null.
func TestZoneResource_emptyMasters(t *testing.T) {
	// given
	_, provider, _ := startMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				// when
				Config: zoneConfig(provider, `
  domain  = "empty.at"
  type    = "master"
  masters = []`),
				// then
				ConfigStateChecks: []statecheck.StateCheck{
					expectAttr("masters", knownvalue.SetSizeExact(0)),
					expectAttr("serial_auto_increment", knownvalue.Bool(true)), // unset keeps the API default
				},
			},
		},
	})
}

// Given an API that reports an error with HTTP 200 and {"status":"failed"} (as
// the real API does), when a zone is changed, then tofu shows the error.
func TestZoneResource_failedStatusWith200(t *testing.T) {
	// given
	_, provider, f := startMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					f.set(respondOn(http.MethodPatch, http.StatusOK, "application/json", `{"status":"failed","message":"nope"}`))
				},
				// when
				Config: zoneConfig(provider, `
  domain   = "example.at"
  type     = "master"
  disabled = true`),
				// then
				ExpectError: regexp.MustCompile(`HTTP 200: status "failed": nope`),
			},
			{PreConfig: f.clear, Config: provider}, // clean up
		},
	})
}

// Given an existing zone, when it is imported with a non-canonical name, then
// the state matches the config and no replace is planned.
func TestZoneResource_importNormalizesDomain(t *testing.T) {
	// given
	_, provider, _ := startMock(t)
	config := zoneConfig(provider, `
  domain = "example.at"
  type   = "master"`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				// when
				Config:        config,
				ResourceName:  zoneAddr,
				ImportState:   true,
				ImportStateId: "Example.AT.",
				// then
				ImportStateVerify: true,
			},
		},
	})
}

// Given an API whose PATCH fails, when a disabled zone is created, then the
// zone is tainted (and replaced on the next apply) instead of being orphaned.
func TestZoneResource_patchFailsAfterCreate(t *testing.T) {
	// given
	mock, provider, f := startMock(t)
	config := zoneConfig(provider, `
  domain   = "example.at"
  type     = "master"
  disabled = true`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() { f.set(respondOn(http.MethodPatch, http.StatusInternalServerError, "text/plain", "boom")) },
				// when
				Config: config,
				// then
				ExpectError: regexp.MustCompile(`HTTP 500: boom`),
			},
			{
				// when the API works again and tofu applies again
				PreConfig: f.clear,
				Config:    config,
				// then the tainted zone is replaced (without state, this would be a create failing with 409)
				ConfigPlanChecks: expectAction(plancheck.ResourceActionDestroyBeforeCreate),
				Check: checkZone(mock, "example.at", func(z mockserver.ZoneDetails) error {
					if !z.ZoneDisabled {
						return fmt.Errorf("zone not disabled in API")
					}
					return nil
				}),
			},
		},
	})
}

// Given an endpoint that answers with HTML and HTTP 200 (proxy, login page,
// wrong URL), when tofu refreshes, then it fails with an error, not a panic.
func TestZoneResource_nonJSONResponse(t *testing.T) {
	// given
	_, provider, f := startMock(t)
	config := zoneConfig(provider, `
  domain = "example.at"
  type   = "master"`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { f.set(respondOn(http.MethodGet, http.StatusOK, "text/html", "<html>login</html>")) },
				// when
				Config: config,
				// then
				ExpectError: regexp.MustCompile(`HTTP 200 without JSON body \(Content-Type "text/html"\)`),
			},
			{PreConfig: f.clear, Config: config}, // clean up
		},
	})
}

// Given an API that answers 404 with an empty body, when tofu refreshes or
// deletes, then the zone counts as gone.
func TestZoneResource_notFoundWithEmptyBody(t *testing.T) {
	// given
	_, provider, f := startMock(t)
	config := zoneConfig(provider, `
  domain = "example.at"
  type   = "master"`)
	empty404 := func(method string) func(http.ResponseWriter, *http.Request) bool {
		return respondOn(method, http.StatusNotFound, "application/json", "")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { f.set(empty404(http.MethodGet)) },
				// when refreshing
				Config:   config,
				PlanOnly: true,
				// then the zone is dropped from state and planned for creation
				ExpectNonEmptyPlan: true,
			},
			{
				PreConfig: func() { f.set(empty404(http.MethodDelete)) },
				// when deleting, then that succeeds
				Config: provider,
			},
		},
	})
}

// Given an API that doesn't find a zone right after creating it, when the zone
// is created, then tofu fails with a clear error.
func TestZoneResource_notFoundAfterCreate(t *testing.T) {
	// given
	_, provider, f := startMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() { f.set(respondOn(http.MethodGet, http.StatusNotFound, "application/json", "")) },
				// when
				Config: zoneConfig(provider, `
  domain = "example.at"
  type   = "master"`),
				// then
				ExpectError: regexp.MustCompile(`zone not found after write`),
			},
		},
	})
}

// Given API errors (zone exists, wrong token), when applying, then tofu shows
// the HTTP status and the API's message.
func TestZoneResource_apiErrors(t *testing.T) {
	// given
	mock, provider, _ := startMock(t)
	if _, err := mock.AddZone(t.Context(), mockserver.AddZoneRequestObject{
		Body: &mockserver.AddNewZoneRequest{Domain: "taken.at", Type: mockserver.Master},
	}); err != nil {
		t.Fatal(err)
	}
	wrongToken := regexp.MustCompile(`api_token = ".*"`).ReplaceAllString(provider, `api_token = "wrong"`)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				// when creating a zone that already exists
				Config: zoneConfig(provider, `
  domain = "taken.at"
  type   = "master"`),
				// then
				ExpectError: regexp.MustCompile(`HTTP 409: Zone taken.at already exists`),
			},
			{
				// when using a wrong token
				Config: zoneConfig(wrongToken, `
  domain = "other.at"
  type   = "master"`),
				// then
				ExpectError: regexp.MustCompile(`HTTP 401`),
			},
		},
	})
}

// zoneConfig returns the provider block plus an rcodezero_zone "test" resource
// with the given attributes.
func zoneConfig(provider, attributes string) string {
	return provider + `
resource "rcodezero_zone" "test" {` + attributes + `
}`
}

func expectAttr(name string, value knownvalue.Check) statecheck.StateCheck {
	return statecheck.ExpectKnownValue(zoneAddr, tfjsonpath.New(name), value)
}

func expectAction(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(zoneAddr, action)},
	}
}

func checkZone(mock *mockserver.Server, domain string, fn func(mockserver.ZoneDetails) error) resource.TestCheckFunc {
	return func(*terraform.State) error {
		z, ok := mock.Zone(domain)
		if !ok {
			return fmt.Errorf("zone %s not found in mock", domain)
		}
		return fn(z)
	}
}

func checkZoneExists(mock *mockserver.Server, domain string) resource.TestCheckFunc {
	return checkZone(mock, domain, func(mockserver.ZoneDetails) error { return nil })
}

func checkZoneGone(mock *mockserver.Server, domain string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if _, ok := mock.Zone(domain); ok {
			return fmt.Errorf("zone %s still exists in mock", domain)
		}
		return nil
	}
}
