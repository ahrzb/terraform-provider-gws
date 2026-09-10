package gws

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// Label is a Gmail label. Only the mutable fields plus the id are modelled.
type Label struct {
	ID                    string `json:"id,omitempty"`
	Name                  string `json:"name,omitempty"`
	LabelListVisibility   string `json:"labelListVisibility,omitempty"`
	MessageListVisibility string `json:"messageListVisibility,omitempty"`
	Type                  string `json:"type,omitempty"`
}

// FilterCriteria mirrors the API object. Every field is omitempty: Gmail distinguishes an
// absent criterion from an empty one, and sending `"from": ""` creates a filter that matches
// nothing rather than one that ignores the sender.
type FilterCriteria struct {
	From           string `json:"from,omitempty"`
	To             string `json:"to,omitempty"`
	Subject        string `json:"subject,omitempty"`
	Query          string `json:"query,omitempty"`
	NegatedQuery   string `json:"negatedQuery,omitempty"`
	HasAttachment  bool   `json:"hasAttachment,omitempty"`
	ExcludeChats   bool   `json:"excludeChats,omitempty"`
	Size           int64  `json:"size,omitempty"`
	SizeComparison string `json:"sizeComparison,omitempty"`
}

type FilterAction struct {
	AddLabelIDs    []string `json:"addLabelIds,omitempty"`
	RemoveLabelIDs []string `json:"removeLabelIds,omitempty"`
	Forward        string   `json:"forward,omitempty"`
}

type Filter struct {
	ID       string          `json:"id,omitempty"`
	Criteria *FilterCriteria `json:"criteria,omitempty"`
	Action   *FilterAction   `json:"action,omitempty"`
}

// --- labels

func (c *Client) CreateLabel(ctx context.Context, l Label) (*Label, error) {
	var out Label
	err := c.do(ctx, http.MethodPost, "/users/"+c.user()+"/labels", l, &out)
	return &out, err
}

func (c *Client) GetLabel(ctx context.Context, id string) (*Label, error) {
	var out Label
	err := c.do(ctx, http.MethodGet, "/users/"+c.user()+"/labels/"+url.PathEscape(id), nil, &out)
	return &out, err
}

// UpdateLabel patches a label. Unlike filters, labels are genuinely mutable, so renaming one
// keeps its id and every message stays labelled.
func (c *Client) UpdateLabel(ctx context.Context, id string, l Label) (*Label, error) {
	var out Label
	err := c.do(ctx, http.MethodPatch, "/users/"+c.user()+"/labels/"+url.PathEscape(id), l, &out)
	return &out, err
}

func (c *Client) DeleteLabel(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/users/"+c.user()+"/labels/"+url.PathEscape(id), nil, nil)
}

func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	var out struct {
		Labels []Label `json:"labels"`
	}
	err := c.do(ctx, http.MethodGet, "/users/"+c.user()+"/labels", nil, &out)
	return out.Labels, err
}

// --- filters
//
// Gmail filters are immutable: the API has create, get, list and delete, and no update. Every
// attribute of gws_gmail_filter is therefore RequiresReplace, and a "change" is a new filter
// with a new id.

func (c *Client) CreateFilter(ctx context.Context, f Filter) (*Filter, error) {
	var out Filter
	err := c.do(ctx, http.MethodPost, "/users/"+c.user()+"/settings/filters", f, &out)
	return &out, err
}

func (c *Client) GetFilter(ctx context.Context, id string) (*Filter, error) {
	var out Filter
	err := c.do(ctx, http.MethodGet, "/users/"+c.user()+"/settings/filters/"+url.PathEscape(id), nil, &out)
	return &out, err
}

func (c *Client) DeleteFilter(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/users/"+c.user()+"/settings/filters/"+url.PathEscape(id), nil, nil)
}

func (c *Client) ListFilters(ctx context.Context) ([]Filter, error) {
	var out struct {
		Filter []Filter `json:"filter"`
	}
	err := c.do(ctx, http.MethodGet, "/users/"+c.user()+"/settings/filters", nil, &out)
	return out.Filter, err
}

// Retry runs fn, retrying on 429 and 5xx with bounded backoff.
//
// Gmail rate limits filter creation after a few dozen in a row, which a Terraform apply of a
// whole filter set hits immediately. Without this, a large apply fails halfway and leaves
// state and account disagreeing.
func Retry(ctx context.Context, attempts int, fn func() error) error {
	var err error
	for i := range attempts {
		if err = fn(); err == nil {
			return nil
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || (apiErr.Status != http.StatusTooManyRequests && apiErr.Status/100 != 5) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1<<i) * time.Second):
		}
	}
	return err
}
