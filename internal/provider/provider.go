// Package provider implements the gws Terraform/OpenTofu provider.
//
// Scope: Google Workspace resources for a single account. Gmail filters and labels are
// implemented; the rest of the Workspace surface (Drive, Calendar, Contacts, settings) is the
// intended direction, which is why resources are named `gws_<service>_<thing>`.
package provider

import (
	"context"
	"os"

	"github.com/ahrzb/terraform-provider-gws/internal/gws"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type gwsProvider struct {
	version string
}

type providerModel struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	RefreshToken types.String `tfsdk:"refresh_token"`
	AccessToken  types.String `tfsdk:"access_token"`
	UserID       types.String `tfsdk:"user_id"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &gwsProvider{version: version} }
}

func (p *gwsProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gws"
	resp.Version = p.version
}

func (p *gwsProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages Google Workspace resources for one account. " +
			"Credentials come from an OAuth client plus a refresh token - the same pair the " +
			"`gws` CLI stores, which `gws auth export` will print.",
		Attributes: map[string]schema.Attribute{
			"client_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "OAuth client id. Falls back to `GWS_CLIENT_ID`.",
			},
			"client_secret": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "OAuth client secret. Falls back to `GWS_CLIENT_SECRET`.",
			},
			"refresh_token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "OAuth refresh token. Falls back to `GWS_REFRESH_TOKEN`. " +
					"Obtained interactively once (`gws auth login`, then `gws auth export`); there is " +
					"no non-interactive way to mint one for a consumer account.",
			},
			"access_token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "A pre-minted access token, used instead of refreshing. Falls back " +
					"to `GWS_ACCESS_TOKEN`. Short lived - meant for CI, not for a desktop.",
			},
			"user_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The mailbox to act on. Defaults to `me`, the owner of the credentials.",
			},
		},
	}
}

func (p *gwsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pick := func(v types.String, env string) string {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
			return v.ValueString()
		}
		return os.Getenv(env)
	}

	client, err := gws.New(gws.Config{
		ClientID:     pick(cfg.ClientID, "GWS_CLIENT_ID"),
		ClientSecret: pick(cfg.ClientSecret, "GWS_CLIENT_SECRET"),
		RefreshToken: pick(cfg.RefreshToken, "GWS_REFRESH_TOKEN"),
		AccessToken:  pick(cfg.AccessToken, "GWS_ACCESS_TOKEN"),
		UserID:       pick(cfg.UserID, "GWS_USER_ID"),
		Endpoint:     os.Getenv("GWS_GMAIL_ENDPOINT"), // tests and local fakes only
	})
	if err != nil {
		resp.Diagnostics.AddError("Incomplete credentials", err.Error()+
			"\n\nSet them in the provider block, or in GWS_CLIENT_ID / GWS_CLIENT_SECRET / "+
			"GWS_REFRESH_TOKEN. `gws auth export` prints all three.")
		return
	}
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *gwsProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewGmailFilterResource,
		NewGmailLabelResource,
	}
}

func (p *gwsProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewGmailLabelsDataSource,
	}
}

// clientFrom pulls the configured client out of the framework's provider data, reporting the
// one failure mode that matters: a resource used with a provider that failed to configure.
func clientFrom(data any, diags interface{ AddError(string, string) }) *gws.Client {
	if data == nil {
		return nil
	}
	client, ok := data.(*gws.Client)
	if !ok {
		diags.AddError("Unexpected provider data", "The provider was not configured correctly; this is a bug in the provider.")
		return nil
	}
	return client
}
