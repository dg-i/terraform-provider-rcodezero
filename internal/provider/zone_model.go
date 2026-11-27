package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dg-i/terraform-provider-rcodezero/internal/rcode0api"
)

type zoneResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Domain              types.String `tfsdk:"domain"`
	Type                types.String `tfsdk:"type"`
	Masters             types.Set    `tfsdk:"masters"`
	Disabled            types.Bool   `tfsdk:"disabled"`
	SerialAutoIncrement types.Bool   `tfsdk:"serial_auto_increment"`
	NSSet               types.List   `tfsdk:"nsset"`
	Created             types.String `tfsdk:"created"`
}

func (m zoneResourceModel) addRequest() rcode0api.AddNewZoneRequest {
	body := rcode0api.AddNewZoneRequest{
		Domain:              m.Domain.ValueString(),
		Type:                rcode0api.TypeWithConsumer(m.Type.ValueString()),
		SerialAutoIncrement: knownBool(m.SerialAutoIncrement),
	}
	if masters := stringsOf(m.Masters); len(masters) > 0 {
		body.Masters = &masters
	}
	return body
}

func (m zoneResourceModel) updateRequest() rcode0api.UpdateZoneRequest {
	// UpdateZoneRequest uses the upper-case enum, unlike AddNewZoneRequest.
	typ := rcode0api.Type(strings.ToUpper(m.Type.ValueString()))
	masters := stringsOf(m.Masters)
	body := rcode0api.UpdateZoneRequest{Type: &typ, Masters: &masters}
	// The spec allows serial_auto_increment for primary zones only.
	if m.Type.ValueString() == zoneTypeMaster {
		body.SerialAutoIncrement = knownBool(m.SerialAutoIncrement)
	}
	return body
}

// needsUpdate reports whether the zone settings changed; zone_disabled has its
// own PATCH and isn't part of it.
func needsUpdate(plan, state zoneResourceModel) bool {
	return !plan.Type.Equal(state.Type) || !plan.Masters.Equal(state.Masters) ||
		(!plan.SerialAutoIncrement.IsUnknown() && !plan.SerialAutoIncrement.Equal(state.SerialAutoIncrement))
}

// setFromAPI copies the zone into m, in the form the user writes in the config.
func (m *zoneResourceModel) setFromAPI(z *rcode0api.ZoneDetails) {
	m.ID = m.Domain
	// The API answers in upper case (SLAVE/MASTER) but takes lower case on create.
	m.Type = types.StringValue(strings.ToLower(string(z.Type)))
	m.Disabled = types.BoolValue(z.ZoneDisabled)
	// Missing for slave zones in the real API.
	m.SerialAutoIncrement = types.BoolPointerValue(z.SerialAutoIncrement)
	m.NSSet = stringList(z.Nsset)
	m.Created = types.StringValue(z.Created)

	switch {
	case len(z.Masters) > 0:
		m.Masters = stringSet(z.Masters)
	case m.Masters.IsNull() || m.Masters.IsUnknown():
		m.Masters = types.SetNull(types.StringType)
	default:
		// Keep `masters = []` from the config as an empty set, not null,
		// otherwise tofu reports an inconsistent result.
		m.Masters = stringSet(nil)
	}
}

// knownBool returns nil for null and unknown values, so the API applies its
// own default. ValueBoolPointer would turn unknown into a pointer to false.
func knownBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueBoolPointer()
}

func stringsOf(set types.Set) []string {
	values := []string{}
	for _, v := range set.Elements() {
		if s, ok := v.(types.String); ok {
			values = append(values, s.ValueString())
		}
	}
	return values
}

func stringSet(values []string) types.Set {
	return types.SetValueMust(types.StringType, stringValues(values))
}

func stringList(values []string) types.List {
	return types.ListValueMust(types.StringType, stringValues(values))
}

func stringValues(values []string) []attr.Value {
	elems := make([]attr.Value, len(values))
	for i, v := range values {
		elems[i] = types.StringValue(v)
	}
	return elems
}
