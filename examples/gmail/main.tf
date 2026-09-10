terraform {
  required_providers {
    gws = {
      source = "registry.opentofu.org/ahrzb/gws"
    }
  }
}

# Credentials come from GWS_CLIENT_ID / GWS_CLIENT_SECRET / GWS_REFRESH_TOKEN.
# `gws auth export` prints all three.
provider "gws" {}

# Labels Terraform did not create - hand-made ones, and the system labels.
data "gws_gmail_labels" "all" {}

resource "gws_gmail_label" "receipts" {
  name = "Money/Receipts"
}

resource "gws_gmail_label" "promos" {
  name                  = "Shopping/Promotions"
  label_list_visibility = "labelShow"
}

# Pending or actionable: labelled, and left in the inbox for a human to clear.
resource "gws_gmail_filter" "receipts" {
  query         = "{from:service@paypal.de (from:apple.com subject:invoice)}"
  add_label_ids = [gws_gmail_label.receipts.id]
}

# A completed record or marketing: labelled and archived. Note the negated query - without it
# this rule would swallow the receipts that the same senders also send.
resource "gws_gmail_filter" "promos" {
  query         = "{from:news.example.com from:mail.example.org}"
  negated_query = "subject:(receipt OR invoice OR refund OR \"password reset\")"

  add_label_ids    = [gws_gmail_label.promos.id]
  remove_label_ids = ["INBOX", "IMPORTANT"]
}

# Pointing at a label that already existed, by name.
resource "gws_gmail_filter" "existing_label" {
  from          = "notifications@github.com"
  add_label_ids = [data.gws_gmail_labels.all.ids["Tech/Accounts"]]
}

output "label_ids" {
  value = data.gws_gmail_labels.all.ids
}
