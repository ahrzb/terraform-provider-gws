package provider

import (
	"context"

	"github.com/ahrzb/terraform-provider-gws/internal/gws"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type gmailFilterResource struct {
	client *gws.Client
}

type gmailFilterModel struct {
	ID             types.String `tfsdk:"id"`
	Query          types.String `tfsdk:"query"`
	NegatedQuery   types.String `tfsdk:"negated_query"`
	From           types.String `tfsdk:"from"`
	To             types.String `tfsdk:"to"`
	Subject        types.String `tfsdk:"subject"`
	HasAttachment  types.Bool   `tfsdk:"has_attachment"`
	ExcludeChats   types.Bool   `tfsdk:"exclude_chats"`
	Size           types.Int64  `tfsdk:"size"`
	SizeComparison types.String `tfsdk:"size_comparison"`
	AddLabelIDs    types.List   `tfsdk:"add_label_ids"`
	RemoveLabelIDs types.List   `tfsdk:"remove_label_ids"`
	Forward        types.String `tfsdk:"forward"`
}

func NewGmailFilterResource() resource.Resource { return &gmailFilterResource{} }

func (r *gmailFilterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gmail_filter"
}

func (r *gmailFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Gmail filters are immutable: the API offers create, get, list and delete, and no update
	// of any kind. So every attribute forces replacement, and Terraform's plan for an edited
	// filter is destroy-then-create with a new id. This is not a modelling shortcut - it is
	// the only behaviour the API permits.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A Gmail filter: criteria that select mail, and an action that " +
			"labels, archives or forwards it.\n\n" +
			"**Filters are immutable server-side.** Any change replaces the filter, and the " +
			"replacement gets a new id.\n\n" +
			"**Filters only run at delivery.** Creating one does nothing to mail that already " +
			"arrived; Gmail has no way to reapply a filter retroactively, and neither does this " +
			"provider. Backfilling is a data operation, not a configuration one.\n\n" +
			"Every matching filter applies - filters are a set, not an ordered chain - so two " +
			"filters matching the same message both add their labels.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"query": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: replace,
				MarkdownDescription: "A Gmail search expression, e.g. `{from:a.com from:b.com}`. " +
					"Note that Gmail matches whole words, not prefixes: `subject:Bestellbest` " +
					"matches nothing at all.",
			},
			"negated_query": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: replace,
				MarkdownDescription: "Mail matching this is excluded. The usual use is keeping " +
					"transactional mail (receipts, password resets) out of a marketing rule.",
			},
			"from":    schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "Structured sender criterion."},
			"to":      schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "Structured recipient criterion."},
			"subject": schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "Structured subject criterion."},
			"has_attachment": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"exclude_chats": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"size": schema.Int64Attribute{
				Optional:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
				MarkdownDescription: "Size in bytes, paired with `size_comparison`.",
			},
			"size_comparison": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: replace,
				Validators:    []validator.String{stringvalidator.OneOf("unspecified", "smaller", "larger")},
			},
			"add_label_ids": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				MarkdownDescription: "Label ids to add. Use `gws_gmail_label.x.id` for your own " +
					"labels, or the system ids: `IMPORTANT`, `STARRED`, `UNREAD`, `SPAM`, `TRASH`, " +
					"`CATEGORY_PERSONAL` and friends.",
			},
			"remove_label_ids": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				MarkdownDescription: "Label ids to remove. Removing `INBOX` is what the Gmail UI " +
					"calls \"Skip the Inbox\"; removing `IMPORTANT` is \"Never mark as important\".",
			},
			"forward": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: replace,
				MarkdownDescription: "Forwarding address. It must already be a verified forwarding " +
					"address on the account, or Gmail rejects the filter.",
			},
		},
	}
}

func (r *gmailFilterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func strs(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	_ = l.ElementsAs(ctx, &out, false)
	return out
}

func (m *gmailFilterModel) toAPI(ctx context.Context) gws.Filter {
	return gws.Filter{
		Criteria: &gws.FilterCriteria{
			Query:          m.Query.ValueString(),
			NegatedQuery:   m.NegatedQuery.ValueString(),
			From:           m.From.ValueString(),
			To:             m.To.ValueString(),
			Subject:        m.Subject.ValueString(),
			HasAttachment:  m.HasAttachment.ValueBool(),
			ExcludeChats:   m.ExcludeChats.ValueBool(),
			Size:           m.Size.ValueInt64(),
			SizeComparison: m.SizeComparison.ValueString(),
		},
		Action: &gws.FilterAction{
			AddLabelIDs:    strs(ctx, m.AddLabelIDs),
			RemoveLabelIDs: strs(ctx, m.RemoveLabelIDs),
			Forward:        m.Forward.ValueString(),
		},
	}
}

// fromAPI refreshes state from the API, preserving the null-vs-empty distinction: a criterion
// the config never set must stay null, or every plan shows a spurious "" -> null diff.
func (m *gmailFilterModel) fromAPI(ctx context.Context, f *gws.Filter) {
	m.ID = types.StringValue(f.ID)

	set := func(got string) types.String {
		if got == "" {
			return types.StringNull()
		}
		return types.StringValue(got)
	}
	c := f.Criteria
	if c == nil {
		c = &gws.FilterCriteria{}
	}
	m.Query = set(c.Query)
	m.NegatedQuery = set(c.NegatedQuery)
	m.From = set(c.From)
	m.To = set(c.To)
	m.Subject = set(c.Subject)
	m.SizeComparison = set(c.SizeComparison)
	m.HasAttachment = types.BoolValue(c.HasAttachment)
	m.ExcludeChats = types.BoolValue(c.ExcludeChats)
	if c.Size == 0 {
		m.Size = types.Int64Null()
	} else {
		m.Size = types.Int64Value(c.Size)
	}

	a := f.Action
	if a == nil {
		a = &gws.FilterAction{}
	}
	m.AddLabelIDs = stringListOrNull(ctx, a.AddLabelIDs)
	m.RemoveLabelIDs = stringListOrNull(ctx, a.RemoveLabelIDs)
	m.Forward = set(a.Forward)
}

func stringListOrNull(ctx context.Context, in []string) types.List {
	if len(in) == 0 {
		return types.ListNull(types.StringType)
	}
	l, _ := types.ListValueFrom(ctx, types.StringType, in)
	return l
}

func (r *gmailFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan gmailFilterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	if plan.Query.ValueString() == "" && plan.From.ValueString() == "" &&
		plan.To.ValueString() == "" && plan.Subject.ValueString() == "" &&
		!plan.HasAttachment.ValueBool() && plan.Size.ValueInt64() == 0 {
		resp.Diagnostics.AddError("Filter has no criteria",
			"A filter with no criteria would match every message. Set at least one of query, from, to, subject, has_attachment or size.")
		return
	}
	var out *gws.Filter
	err := gws.Retry(ctx, 5, func() error {
		var e error
		out, e = r.client.CreateFilter(ctx, plan.toAPI(ctx))
		return e
	})
	if err != nil {
		resp.Diagnostics.AddError("Creating filter failed", err.Error())
		return
	}
	plan.ID = types.StringValue(out.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *gmailFilterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state gmailFilterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	out, err := r.client.GetFilter(ctx, state.ID.ValueString())
	if err != nil {
		if gws.NotFound(err) {
			// Deleted in the Gmail UI. Drop it so the next apply recreates it.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Reading filter failed", err.Error())
		return
	}
	state.fromAPI(ctx, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update exists only to satisfy the interface. Every attribute is RequiresReplace, so the
// framework never routes a change here; if it somehow does, failing loudly beats silently
// leaving the account and state disagreeing.
func (r *gmailFilterResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Filters cannot be updated",
		"Gmail filters are immutable and every attribute forces replacement. Reaching Update is a bug in the provider.")
}

func (r *gmailFilterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state gmailFilterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	// Deleting a filter stops it acting on future mail; labels it already applied stay put.
	if err := r.client.DeleteFilter(ctx, state.ID.ValueString()); err != nil && !gws.NotFound(err) {
		resp.Diagnostics.AddError("Deleting filter failed", err.Error())
	}
}

func (r *gmailFilterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
