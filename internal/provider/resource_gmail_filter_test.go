package provider

import (
	"context"
	"testing"

	"github.com/ahrzb/terraform-provider-gws/internal/gws"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestFromAPILeavesUnsetCriteriaNull(t *testing.T) {
	ctx := context.Background()
	var m gmailFilterModel
	m.fromAPI(ctx, &gws.Filter{
		ID:       "f1",
		Criteria: &gws.FilterCriteria{Query: "from:a.com"},
		Action:   &gws.FilterAction{AddLabelIDs: []string{"Label_1"}},
	})

	// If an absent criterion came back as "" instead of null, every subsequent plan would
	// show a permanent diff against a config that never set it - and because every attribute
	// is RequiresReplace, that diff would destroy and recreate the filter on each apply.
	if !m.From.IsNull() {
		t.Errorf("from = %v, want null", m.From)
	}
	if !m.Subject.IsNull() {
		t.Errorf("subject = %v, want null", m.Subject)
	}
	if !m.NegatedQuery.IsNull() {
		t.Errorf("negated_query = %v, want null", m.NegatedQuery)
	}
	if !m.Size.IsNull() {
		t.Errorf("size = %v, want null", m.Size)
	}
	if !m.RemoveLabelIDs.IsNull() {
		t.Errorf("remove_label_ids = %v, want null", m.RemoveLabelIDs)
	}
	if m.Query.ValueString() != "from:a.com" {
		t.Errorf("query = %v", m.Query)
	}
}

func TestRoundTripThroughAPIShapeIsStable(t *testing.T) {
	ctx := context.Background()
	original := gmailFilterModel{
		Query:          types.StringValue("{from:a.com from:b.com}"),
		NegatedQuery:   types.StringValue("subject:(receipt)"),
		AddLabelIDs:    stringSetOrNull(ctx, []string{"Label_7"}),
		RemoveLabelIDs: stringSetOrNull(ctx, []string{"INBOX", "IMPORTANT"}),
		HasAttachment:  types.BoolValue(false),
		ExcludeChats:   types.BoolValue(false),
	}
	sent := original.toAPI(ctx)
	sent.ID = "f2"

	var back gmailFilterModel
	back.fromAPI(ctx, &sent)

	// A plan right after an apply must be empty. That holds only if what we send and what we
	// read back model identically.
	if back.Query != original.Query {
		t.Errorf("query: %v -> %v", original.Query, back.Query)
	}
	if back.NegatedQuery != original.NegatedQuery {
		t.Errorf("negated_query: %v -> %v", original.NegatedQuery, back.NegatedQuery)
	}
	if back.AddLabelIDs.String() != original.AddLabelIDs.String() {
		t.Errorf("add_label_ids: %v -> %v", original.AddLabelIDs, back.AddLabelIDs)
	}
	if back.RemoveLabelIDs.String() != original.RemoveLabelIDs.String() {
		t.Errorf("remove_label_ids: %v -> %v", original.RemoveLabelIDs, back.RemoveLabelIDs)
	}
}

func TestArchiveIsExpressedAsRemovingInbox(t *testing.T) {
	ctx := context.Background()
	m := gmailFilterModel{
		Query:          types.StringValue("from:newsletter.example"),
		AddLabelIDs:    stringSetOrNull(ctx, []string{"Label_9"}),
		RemoveLabelIDs: stringSetOrNull(ctx, []string{"INBOX"}),
	}
	got := m.toAPI(ctx)
	if len(got.Action.RemoveLabelIDs) != 1 || got.Action.RemoveLabelIDs[0] != "INBOX" {
		t.Fatalf("remove = %v; skipping the inbox is removing the INBOX label, nothing else", got.Action.RemoveLabelIDs)
	}
	if got.Criteria.Query != "from:newsletter.example" {
		t.Errorf("query = %q", got.Criteria.Query)
	}
}

func TestNilActionAndCriteriaDoNotPanic(t *testing.T) {
	ctx := context.Background()
	var m gmailFilterModel
	// A filter created in the web UI can come back with no action at all.
	m.fromAPI(ctx, &gws.Filter{ID: "f3"})
	if m.ID.ValueString() != "f3" {
		t.Errorf("id = %v", m.ID)
	}
	if !m.AddLabelIDs.IsNull() {
		t.Errorf("add_label_ids = %v, want null", m.AddLabelIDs)
	}
}

func TestLabelOrderDoesNotMatter(t *testing.T) {
	ctx := context.Background()
	// Gmail returns removeLabelIds in its own order. Modelling these as lists made
	// ["INBOX","IMPORTANT"] differ from ["IMPORTANT","INBOX"], which planned as 43 filters
	// destroyed and recreated on an import of an unchanged account.
	declared := gmailFilterModel{
		Query:          types.StringValue("from:example.com"),
		RemoveLabelIDs: stringSetOrNull(ctx, []string{"INBOX", "IMPORTANT"}),
	}
	returned := gws.Filter{
		ID:       "f1",
		Criteria: &gws.FilterCriteria{Query: "from:example.com"},
		Action:   &gws.FilterAction{RemoveLabelIDs: []string{"IMPORTANT", "INBOX"}},
	}
	var back gmailFilterModel
	back.fromAPI(ctx, &returned)
	if !back.RemoveLabelIDs.Equal(declared.RemoveLabelIDs) {
		t.Errorf("a reordered label set must compare equal:\n  declared %v\n  returned %v",
			declared.RemoveLabelIDs, back.RemoveLabelIDs)
	}
}
