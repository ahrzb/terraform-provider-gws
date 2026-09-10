package provider

import (
	"context"

	"github.com/ahrzb/terraform-provider-gws/internal/gws"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type gmailLabelResource struct {
	client *gws.Client
}

type gmailLabelModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	LabelListVisibility   types.String `tfsdk:"label_list_visibility"`
	MessageListVisibility types.String `tfsdk:"message_list_visibility"`
}

func NewGmailLabelResource() resource.Resource { return &gmailLabelResource{} }

func (r *gmailLabelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gmail_label"
}

func (r *gmailLabelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Gmail label. Nesting is expressed in the name with `/`, e.g. " +
			"`Shopping/Orders`; Gmail has no separate parent object.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Gmail's label id, e.g. `Label_42`. Filters reference labels by this.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name. Renaming keeps the id, so labelled mail stays labelled.",
			},
			"label_list_visibility": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("labelShow"),
				MarkdownDescription: "`labelShow`, `labelShowIfUnread` or `labelHide`.",
				Validators: []validator.String{
					stringvalidator.OneOf("labelShow", "labelShowIfUnread", "labelHide"),
				},
			},
			"message_list_visibility": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("show"),
				MarkdownDescription: "`show` or `hide` - whether the label chip appears on messages.",
				Validators: []validator.String{
					stringvalidator.OneOf("show", "hide"),
				},
			},
		},
	}
}

func (r *gmailLabelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *gmailLabelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan gmailLabelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	var out *gws.Label
	err := gws.Retry(ctx, 5, func() error {
		var e error
		out, e = r.client.CreateLabel(ctx, gws.Label{
			Name:                  plan.Name.ValueString(),
			LabelListVisibility:   plan.LabelListVisibility.ValueString(),
			MessageListVisibility: plan.MessageListVisibility.ValueString(),
		})
		return e
	})
	if err != nil {
		resp.Diagnostics.AddError("Creating label failed", err.Error())
		return
	}
	plan.ID = types.StringValue(out.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *gmailLabelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state gmailLabelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	out, err := r.client.GetLabel(ctx, state.ID.ValueString())
	if err != nil {
		if gws.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading label failed", err.Error())
		return
	}
	state.Name = types.StringValue(out.Name)
	if out.LabelListVisibility != "" {
		state.LabelListVisibility = types.StringValue(out.LabelListVisibility)
	}
	if out.MessageListVisibility != "" {
		state.MessageListVisibility = types.StringValue(out.MessageListVisibility)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *gmailLabelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state gmailLabelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	out, err := r.client.UpdateLabel(ctx, state.ID.ValueString(), gws.Label{
		Name:                  plan.Name.ValueString(),
		LabelListVisibility:   plan.LabelListVisibility.ValueString(),
		MessageListVisibility: plan.MessageListVisibility.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Updating label failed", err.Error())
		return
	}
	plan.ID = types.StringValue(out.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *gmailLabelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state gmailLabelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	// Deleting a label removes it from every message it was on. Terraform cannot warn at
	// destroy time, so this is called out in the docs instead.
	if err := r.client.DeleteLabel(ctx, state.ID.ValueString()); err != nil && !gws.NotFound(err) {
		resp.Diagnostics.AddError("Deleting label failed", err.Error())
	}
}

func (r *gmailLabelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
