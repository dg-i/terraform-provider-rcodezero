package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dg-i/terraform-provider-rcodezero/internal/rcode0api"
)

const (
	zoneTypeMaster = "master"
	zoneTypeSlave  = "slave"
)

// domainPattern enforces the canonical form (lower case, no trailing dot).
// Any other spelling would differ from what import and the API use, and since
// domain forces replacement, that diff would destroy and recreate the zone.
var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

var (
	_ resource.ResourceWithConfigure      = (*zoneResource)(nil)
	_ resource.ResourceWithImportState    = (*zoneResource)(nil)
	_ resource.ResourceWithValidateConfig = (*zoneResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*zoneResource)(nil)
)

type zoneResource struct {
	client *rcode0api.ClientWithResponses
}

func NewZoneResource() resource.Resource {
	return &zoneResource{}
}

func (r *zoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_zone"
}

func (r *zoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A zone on the RcodeZero Anycast DNS network. Deleting it removes the zone from public DNS; consider lifecycle.prevent_destroy.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:   "Same as domain.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				Description:   "Zone name in lower case without trailing dot, e.g. example.at. Changing it replaces the zone.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(domainPattern, "must be a lower-case zone name without trailing dot, e.g. example.at"),
				},
			},
			"type": schema.StringAttribute{
				Description: "master (records managed at RcodeZero) or slave (transferred from masters).",
				Required:    true,
				Validators:  []validator.String{stringvalidator.OneOf(zoneTypeMaster, zoneTypeSlave)},
			},
			"masters": schema.SetAttribute{
				Description: "IP addresses of the primary nameservers. Required for slave zones, not allowed for master zones.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"disabled": schema.BoolAttribute{
				Description: "Disabled zones stay configured but are not served in DNS. Master zones only.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"serial_auto_increment": schema.BoolAttribute{
				Description: "Increment the serial on RRset updates. Master zones only, null for slave zones. If never set, the API default (true) applies; " +
					"removing it from the config later keeps the current value.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"nsset": schema.ListAttribute{
				Description:   "Nameservers serving the zone, for delegation at the parent.",
				ElementType:   types.StringType,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"created": schema.StringAttribute{
				Description:   "Timestamp when the zone was added to RcodeZero, as returned by the API.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *zoneResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg zoneResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Type.IsUnknown() || cfg.Masters.IsUnknown() {
		return
	}
	hasMasters := len(cfg.Masters.Elements()) > 0
	switch cfg.Type.ValueString() {
	case zoneTypeSlave:
		if !hasMasters {
			resp.Diagnostics.AddAttributeError(path.Root("masters"), "Missing masters", "Slave zones need at least one master.")
		}
		if !cfg.SerialAutoIncrement.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("serial_auto_increment"), "Not allowed for slave zones",
				"serial_auto_increment only applies to master zones.")
		}
		// The API rejects zone_disabled for slave zones (verified 2026-11-19).
		if cfg.Disabled.ValueBool() {
			resp.Diagnostics.AddAttributeError(path.Root("disabled"), "Not allowed for slave zones",
				"disabled only applies to master zones.")
		}
	case zoneTypeMaster:
		if hasMasters {
			resp.Diagnostics.AddAttributeError(path.Root("masters"), "Unexpected masters", "Master zones must not set masters.")
		}
	}
}

// ModifyPlan predicts serial_auto_increment when it isn't configured: the API
// omits it for slave zones, and a switch to master applies the API default.
// UseStateForUnknown alone would carry the old value over a type change.
func (r *zoneResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var cfg, plan zoneResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !cfg.SerialAutoIncrement.IsNull() {
		return
	}

	switch {
	case plan.Type.IsUnknown():
		plan.SerialAutoIncrement = types.BoolUnknown()
	case plan.Type.ValueString() == zoneTypeSlave:
		plan.SerialAutoIncrement = types.BoolNull()
	case !req.State.Raw.IsNull():
		var state zoneResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if !state.Type.Equal(plan.Type) {
			plan.SerialAutoIncrement = types.BoolUnknown()
		}
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}

func (r *zoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*rcode0api.ClientWithResponses)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *zoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan zoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := plan.Domain.ValueString()
	wantDisabled := plan.Disabled.ValueBool()

	httpResp, err := r.client.AddZone(ctx, nil, plan.addRequest())
	if _, err := parseResponse(httpResp, err, http.StatusCreated, rcode0api.ParseAddZoneResult); err != nil {
		resp.Diagnostics.AddError("Failed to create zone "+domain, err.Error())
		return
	}

	// The zone exists from here on. Store it before any follow-up call can
	// fail: an error with state set taints the resource, an error without
	// state would orphan the zone.
	if err := r.readAfterWrite(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to read zone "+domain, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)

	if !wantDisabled {
		return
	}
	if err := r.setDisabled(ctx, domain, true); err != nil {
		resp.Diagnostics.AddError("Failed to disable zone "+domain, err.Error())
		return
	}
	if err := r.readAfterWrite(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to read zone "+domain, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *zoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state zoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := state.Domain.ValueString()

	found, err := r.read(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read zone "+domain, err.Error())
		return
	}
	if !found {
		tflog.Warn(ctx, "zone no longer exists, removing from state", map[string]any{"domain": domain})
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *zoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state zoneResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := plan.Domain.ValueString()
	wantDisabled := plan.Disabled.ValueBool()
	wasDisabled := state.Disabled.ValueBool()

	// zone_disabled only works on master zones: re-enable before a possible
	// switch to slave, disable only after a possible switch to master.
	if wasDisabled && !wantDisabled {
		if err := r.setDisabled(ctx, domain, false); err != nil {
			resp.Diagnostics.AddError("Failed to enable zone "+domain, err.Error())
			return
		}
	}
	if needsUpdate(plan, state) {
		httpResp, err := r.client.UpdateZone(ctx, domain, plan.updateRequest())
		if _, err := parseResponse(httpResp, err, http.StatusOK, rcode0api.ParseUpdateZoneResult); err != nil {
			resp.Diagnostics.AddError("Failed to update zone "+domain, err.Error())
			return
		}
	}
	if wantDisabled && !wasDisabled {
		if err := r.setDisabled(ctx, domain, true); err != nil {
			resp.Diagnostics.AddError("Failed to disable zone "+domain, err.Error())
			return
		}
	}

	if err := r.readAfterWrite(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Failed to read zone "+domain, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *zoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state zoneResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := state.Domain.ValueString()

	httpResp, err := r.client.DeleteZone(ctx, domain)
	_, err = parseResponse(httpResp, err, http.StatusOK, rcode0api.ParseDeleteZoneResult)
	if isNotFound(err) {
		tflog.Warn(ctx, "zone already gone", map[string]any{"domain": domain})
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete zone "+domain, err.Error())
	}
}

func (r *zoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Accept "Example.AT." but store the canonical form the config must use.
	domain := strings.ToLower(strings.TrimSuffix(req.ID, "."))
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), domain)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), domain)...)
}

// read refreshes m (identified by m.Domain) from the API. found is false if
// the zone doesn't exist.
func (r *zoneResource) read(ctx context.Context, m *zoneResourceModel) (found bool, err error) {
	httpResp, err := r.client.GetZoneDetails(ctx, m.Domain.ValueString())
	res, err := parseResponse(httpResp, err, http.StatusOK, rcode0api.ParseGetZoneDetailsResult)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if res.JSON200 == nil {
		return false, fmt.Errorf("HTTP 200 without JSON body (Content-Type %q). Is the endpoint correct?", res.ContentType())
	}
	m.setFromAPI(res.JSON200)
	return true, nil
}

// readAfterWrite is read for Create and Update, where a missing zone is an error.
func (r *zoneResource) readAfterWrite(ctx context.Context, m *zoneResourceModel) error {
	found, err := r.read(ctx, m)
	if err == nil && !found {
		return errors.New("zone not found after write: RcodeZero returned 404 right after creating or updating it")
	}
	return err
}

func (r *zoneResource) setDisabled(ctx context.Context, domain string, disabled bool) error {
	httpResp, err := r.client.PatchZone(ctx, domain, rcode0api.PatchZoneRequest{ZoneDisabled: &disabled})
	_, err = parseResponse(httpResp, err, http.StatusOK, rcode0api.ParsePatchZoneResult)
	return err
}
