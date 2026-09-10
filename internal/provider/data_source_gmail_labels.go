package provider

import (
	"context"

	"github.com/ahrzb/terraform-provider-gws/internal/gws"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type gmailLabelsDataSource struct {
	client *gws.Client
}

type gmailLabelsModel struct {
	IDs    types.Map `tfsdk:"ids"`
	System types.Map `tfsdk:"system"`
}

func NewGmailLabelsDataSource() datasource.DataSource { return &gmailLabelsDataSource{} }

func (d *gmailLabelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gmail_labels"
}

func (d *gmailLabelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every label on the account, as name -> id maps. Use this to point " +
			"filters at labels you did not create with Terraform - hand-made ones, or the ones " +
			"an import brought along.",
		Attributes: map[string]schema.Attribute{
			"ids": schema.MapAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "User labels: display name -> id.",
			},
			"system": schema.MapAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "System labels (`INBOX`, `SPAM`, `CATEGORY_*`): name -> id, which for these are equal.",
			},
		},
	}
}

func (d *gmailLabelsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *gmailLabelsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}
	labels, err := d.client.ListLabels(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing labels failed", err.Error())
		return
	}
	user, system := map[string]string{}, map[string]string{}
	for _, l := range labels {
		if l.Type == "system" {
			system[l.Name] = l.ID
			continue
		}
		user[l.Name] = l.ID
	}
	var state gmailLabelsModel
	idsMap, d1 := types.MapValueFrom(ctx, types.StringType, user)
	sysMap, d2 := types.MapValueFrom(ctx, types.StringType, system)
	resp.Diagnostics.Append(d1...)
	resp.Diagnostics.Append(d2...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.IDs = idsMap
	state.System = sysMap
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
