package provider

import (
	"cmp"
	"context"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dg-i/terraform-provider-rcodezero/internal/rcode0api"
)

const defaultEndpoint = "https://my.rcodezero.at"

var _ provider.Provider = (*rcodezeroProvider)(nil)

type rcodezeroProvider struct {
	version string
}

type rcodezeroProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIToken types.String `tfsdk:"api_token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &rcodezeroProvider{version: version}
	}
}

func (p *rcodezeroProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "rcodezero"
	resp.Version = p.version
}

func (p *rcodezeroProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages zones on the RcodeZero Anycast DNS network.",
		MarkdownDescription: "Manages zones on the [RcodeZero](https://www.rcodezero.at/) Anycast DNS network " +
			"via its REST API v2.\n\n" +
			"## Known limitations\n\n" +
			"- Only zones are managed (`rcodezero_zone`, master and slave). RRsets and DNSSEC are not supported yet.\n" +
			"- The provider is configured from the provider block only, not from environment variables. " +
			"Pass the token as a variable, e.g. via `TF_VAR_rcodezero_api_token`.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Description: "Base URL of the RcodeZero API. Defaults to " + defaultEndpoint + ".",
				Optional:    true,
			},
			"api_token": schema.StringAttribute{
				Description: "RcodeZero API token.",
				Required:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *rcodezeroProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg rcodezeroProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Endpoint.IsUnknown() || cfg.APIToken.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration",
			"endpoint and api_token must be known at plan time.")
		return
	}

	// Configuration comes from the provider block only, never from the
	// environment, so what tofu talks to is always visible in the config.
	endpoint := cmp.Or(cfg.Endpoint.ValueString(), defaultEndpoint)
	token := cfg.APIToken.ValueString()
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_token"), "Empty API token", "api_token must not be empty.")
		return
	}

	client, err := rcode0api.NewClientWithResponses(endpoint,
		rcode0api.WithHTTPClient(&http.Client{
			Timeout:   30 * time.Second,
			Transport: loggingTransport{next: http.DefaultTransport},
		}),
		rcode0api.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("User-Agent", "terraform-provider-rcodezero/"+p.version)
			return nil
		}),
	)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create RcodeZero client", err.Error())
		return
	}
	tflog.Debug(ctx, "configured RcodeZero client", map[string]any{"endpoint": endpoint})
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *rcodezeroProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewZoneResource}
}

func (p *rcodezeroProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
