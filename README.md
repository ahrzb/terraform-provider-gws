# terraform-provider-gws

Declarative Google Workspace resources for OpenTofu and Terraform.

**Today it manages Gmail filters and labels.** The aim is the rest of the Workspace surface —
Drive, Calendar, Contacts, account settings — which is why resources are named
`gws_<service>_<thing>`: adding `gws_drive_permission` later renames nothing.

```hcl
terraform {
  required_providers {
    gws = {
      source = "registry.opentofu.org/ahrzb/gws"
    }
  }
}

provider "gws" {}  # credentials from GWS_CLIENT_ID / GWS_CLIENT_SECRET / GWS_REFRESH_TOKEN

resource "gws_gmail_label" "orders" {
  name = "Shopping/Orders"
}

# Order confirmations: something is in flight, so label it and leave it in the inbox.
resource "gws_gmail_filter" "orders" {
  query         = "{from:bestellbestaetigung@amazon.de (from:thomann.de subject:order)}"
  add_label_ids = [gws_gmail_label.orders.id]
}

# Marketing: label it and skip the inbox. "Skip the Inbox" is removing the INBOX label.
resource "gws_gmail_filter" "promos" {
  query            = "{from:news.example.com from:mail.example.org}"
  negated_query    = "subject:(receipt OR invoice OR \"password reset\")"
  add_label_ids    = [gws_gmail_label.promos.id]
  remove_label_ids = ["INBOX", "IMPORTANT"]
}
```

## Two things the API forces on the design

**Filters are immutable.** Gmail's API has create, get, list and delete — no update of any
kind. So every attribute of `gws_gmail_filter` is `RequiresReplace`, and editing a filter
plans as destroy-then-create with a new id. This is not a modelling shortcut; it is the only
behaviour available. Labels, by contrast, are genuinely mutable: renaming one keeps its id, so
labelled mail stays labelled.

**Filters only run at delivery.** Nothing reapplies a filter to mail that already arrived, so
`apply` never touches history. Backfilling existing mail is a data operation, deliberately out
of scope here — a `terraform apply` that silently relabelled twenty thousand messages would be
a bad surprise. Use the Gmail UI or a script for that.

Related: filters are a set, not an ordered chain. **Every** matching filter applies, so two
filters that match the same message both add their labels. There is no precedence to tune, and
this is the most common way a filter set goes subtly wrong.

## Credentials

The provider needs an OAuth client and a refresh token. Getting a refresh token requires a
browser once, and there is no way around that for a consumer account — so the provider does
not implement an OAuth flow; it consumes the credentials the
[`gws` CLI](https://github.com/googleworkspace/cli) already stores:

```console
$ gws auth login          # once, interactive
$ gws auth export         # prints client_id, client_secret, refresh_token as JSON
```

Feed those in as `GWS_CLIENT_ID`, `GWS_CLIENT_SECRET` and `GWS_REFRESH_TOKEN`, or set them in
the provider block. For CI, `GWS_ACCESS_TOKEN` accepts a pre-minted short-lived token instead.

Required scope: `https://www.googleapis.com/auth/gmail.settings.basic` for filters and labels.

## Resources

| | |
|---|---|
| `gws_gmail_label` | a label; nesting is just `/` in the name. Deleting one strips it from every message it was on |
| `gws_gmail_filter` | criteria + action; immutable, replace-only |
| `gws_gmail_labels` (data) | every label as `name -> id` maps, for pointing filters at labels Terraform did not create |

Both resources support `import` by id:

```console
$ tofu import gws_gmail_filter.promos ANe1Bmh...
```

## Use with Nix

The flake exposes the provider package, an overlay, an OpenTofu with it preinstalled, and a
terranix module.

```nix
{
  inputs.gws-provider.url = "github:ahrzb/terraform-provider-gws";

  # …then either take the ready-made tofu:
  #   inputs.gws-provider.packages.${system}.tofu
  # or add it to your own withPlugins call:
  #   pkgs.opentofu.withPlugins (p: [ p.hashicorp_google gws-provider.packages.${system}.default ])
}
```

The package carries `passthru.provider-source-address = "registry.opentofu.org/ahrzb/gws"`,
which is how `withPlugins` lays out the plugin directory and how OpenTofu resolves `source`.
The provider is not published to a registry — Nix is the distribution channel.

### The terranix module

`terranixModules.gmail` gives the filters a schema, so a rule says what it means instead of
repeating label lookups and add/remove mechanics at every call site:

```nix
# modules = [ ./infra inputs.gws-provider.terranixModules.gmail ];

gws.gmail.filters = [
  {
    name = "orders";
    label = "Shopping/Orders";          # by display name; resolved via the data source
    query = "from:bestellbestaetigung@amazon.de";
  }
  {
    name = "promos";
    label = "Shopping/Promotions";
    query = "{from:news.example.com from:mail.example.org}";
    unless = "subject:(receipt OR invoice)";
    archive = true;                     # skip the inbox
    neverImportant = true;
  }
];
```

It also carries the check the resources cannot express: **two rules claiming one sender with
different labels**. Gmail applies every matching filter, so both labels land on the message
and nothing in the UI shows you why. The module fails at evaluation:

```console
error: gws.gmail: a sender is claimed by rules with different labels, and Gmail applies both:
    vinted.de -> Shopping/Promotions + Feed (rules: vinted-marketing, conflict-probe)
```

Labels are not managed by the module, deliberately: deleting a `gws_gmail_label` strips it
from every message it was ever on, so a `destroy` would be a data-loss event. They are looked
up by name through `gws_gmail_labels`, and the module can only add and remove filters.

## Development

```console
$ nix develop          # go, gopls, gcc (cgo, for test binaries), opentofu, gofumpt
$ go test ./...        # offline: the client is tested against an httptest fake
$ nix flake check      # build + tests
$ nix build .#default  # the provider binary
```

Gmail is not mocked wholesale — the tests cover the parts that have actually broken things:
that an unset criterion never reaches the wire as `""` (which would create a filter matching
nothing), that a 404 is distinguishable from a real error so `Read` can drop a deleted resource
instead of failing, that rate limiting is retried while a 400 is not, and that what is sent and
what is read back model identically, without which every plan would show a phantom diff and —
because filters are replace-only — destroy and recreate the whole set on each apply.

## Licence

MIT.
