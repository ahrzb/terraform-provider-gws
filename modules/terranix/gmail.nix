# A terranix module for Gmail filters.
#
# The provider gives you resources; this gives you a schema. Writing filters directly as
# `gws_gmail_filter` resources means repeating the same mechanics at every call site: looking
# a label id up by name, remembering that "skip the inbox" is removing the INBOX label, and
# spelling out add/remove lists. Here a rule says what it means - `archive = true` - and the
# module knows what that costs in API terms.
#
# It also carries the check that matters: two rules claiming one sender with different labels.
# Gmail applies *every* matching filter, so both labels land on the message, and nothing in
# the Gmail UI shows you why. As an assertion it fails at evaluation - before plan, let alone
# apply.
#
# Labels are deliberately not managed. Deleting a `gws_gmail_label` removes it from every
# message it was ever on, which would make `tofu destroy` a data-loss event; they are looked
# up by name through the data source instead, so this module can only add and remove filters.
{ lib, config, ... }:
let
  cfg = config.gws.gmail;
  inherit (lib) mkOption mkEnableOption types;

  systemLabels = [
    "INBOX"
    "IMPORTANT"
    "SPAM"
    "TRASH"
    "UNREAD"
    "STARRED"
  ];
  isSystem = n: builtins.elem n systemLabels || lib.hasPrefix "CATEGORY_" n;
  labelRef =
    n: if isSystem n then n else ''''${data.gws_gmail_labels.${cfg.dataSourceName}.ids["${n}"]}'';

  # Terraform resource names may not begin with a digit, and rule names are human strings.
  resourceName =
    name:
    let
      safe = lib.stringAsChars (c: if builtins.match "[A-Za-z0-9_-]" c != null then c else "_") name;
    in
    if builtins.match "[0-9].*" safe != null then "_${safe}" else safe;

  toResource = rule: {
    name = resourceName rule.name;
    value = lib.filterAttrs (_: v: v != null && v != [ ]) {
      query = rule.query;
      negated_query = rule.unless;
      from = rule.match.from;
      to = rule.match.to;
      subject = rule.match.subject;
      add_label_ids = map labelRef (lib.optional (rule.label != null) rule.label ++ rule.extraLabels);
      remove_label_ids =
        lib.optional rule.archive "INBOX"
        ++ lib.optional rule.neverImportant "IMPORTANT"
        ++ lib.optional rule.neverSpam "SPAM";
    };
  };

  # --- the lint ------------------------------------------------------------------------

  # Split a query into top-level terms, respecting {} and (). `{from:a (from:b subject:c)}`
  # yields two terms, not four: naive splitting would report b as unscoped, the opposite of
  # the truth.
  terms =
    query:
    let
      step =
        acc: c:
        if c == "(" || c == "{" then
          acc
          // {
            depth = acc.depth + 1;
            cur = acc.cur + c;
          }
        else if c == ")" || c == "}" then
          acc
          // {
            depth = acc.depth - 1;
            cur = acc.cur + c;
          }
        else if c == " " && acc.depth == 0 then
          acc
          // {
            out = acc.out ++ lib.optional (acc.cur != "") acc.cur;
            cur = "";
          }
        else
          acc // { cur = acc.cur + c; };
      inner =
        if lib.hasPrefix "{" query && lib.hasSuffix "}" query then
          builtins.substring 1 (builtins.stringLength query - 2) query
        else
          query;
      done = builtins.foldl' step {
        depth = 0;
        cur = "";
        out = [ ];
      } (lib.stringToCharacters inner);
    in
    done.out ++ lib.optional (done.cur != "") done.cur;

  # A sender is scoped when a `subject:` is ANDed with it. Inside a `{...}` OR-list each
  # alternative must carry its own qualifier, because the alternatives are independent.
  sendersOf =
    rule:
    let
      q = if rule.query == null then "" else rule.query;
      ts = terms q;
      anded = !(lib.hasPrefix "{" q);
      scopedWhole = anded && lib.any (t: lib.hasPrefix "subject:" t || lib.hasPrefix "filename:" t) ts;
      fromsIn =
        t:
        lib.optionals (!(lib.hasInfix "subject:" t)) (
          map (lib.removePrefix "from:") (
            lib.filter (lib.hasPrefix "from:") (
              lib.splitString " " (lib.replaceStrings [ "(" ")" ] [ "" "" ] t)
            )
          )
        );
    in
    if scopedWhole || rule.label == null then [ ] else lib.concatMap fromsIn ts;

  claims = lib.concatMap (
    r:
    map (s: {
      sender = s;
      inherit (r) label name;
    }) (sendersOf r)
  ) cfg.filters;
  bySender = lib.groupBy (c: c.sender) claims;
  conflicts = lib.filter (s: lib.length (lib.unique (map (c: c.label) bySender.${s})) > 1) (
    builtins.attrNames bySender
  );
  conflictReport = lib.concatMapStringsSep "\n" (
    s:
    "    ${s} -> ${lib.concatStringsSep " + " (lib.unique (map (c: c.label) bySender.${s}))}"
    + " (rules: ${lib.concatMapStringsSep ", " (c: c.name) bySender.${s}})"
  ) conflicts;

  duplicateNames = lib.subtractLists (lib.unique (map (r: r.name) cfg.filters)) (
    map (r: r.name) cfg.filters
  );

  checked =
    lib.throwIf (conflicts != [ ])
      ''
        gws.gmail: a sender is claimed by rules with different labels, and Gmail applies both:
        ${conflictReport}
          Scope one of them with a subject: term, or give it an `unless`.''
      (
        lib.throwIf (duplicateNames != [ ]) "gws.gmail: duplicate filter names: ${toString duplicateNames}"
          (builtins.listToAttrs (map toResource cfg.filters))
      );

  filterType = types.submodule {
    options = {
      name = mkOption {
        type = types.str;
        description = "Human handle; becomes the Terraform resource name.";
      };
      label = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = "Label to add, by display name. Looked up through the data source.";
      };
      query = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = ''
          A Gmail search expression. Gmail matches whole words, not prefixes:
          `subject:Bestellbest` matches nothing at all.
        '';
      };
      unless = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = "negatedQuery: mail matching this is excluded from the rule.";
      };
      match = {
        from = mkOption {
          type = types.nullOr types.str;
          default = null;
          description = "Structured sender criterion, for filters that do not use a query.";
        };
        to = mkOption {
          type = types.nullOr types.str;
          default = null;
        };
        subject = mkOption {
          type = types.nullOr types.str;
          default = null;
        };
      };
      archive = mkOption {
        type = types.bool;
        default = false;
        description = "Skip the inbox - i.e. remove the INBOX label on arrival.";
      };
      neverImportant = mkOption {
        type = types.bool;
        default = false;
        description = "Never mark as important - i.e. remove the IMPORTANT label.";
      };
      neverSpam = mkOption {
        type = types.bool;
        default = false;
      };
      extraLabels = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = "Further labels or system flags to add, e.g. `IMPORTANT`, `CATEGORY_PERSONAL`.";
      };
    };
  };
in
{
  options.gws.gmail = {
    enable = mkEnableOption "Gmail filters managed through the gws provider" // {
      default = cfg.filters != [ ];
    };

    filters = mkOption {
      type = types.listOf filterType;
      default = [ ];
      description = "Filter rules. Order is irrelevant: Gmail applies every matching filter.";
    };

    sourceAddress = mkOption {
      type = types.str;
      default = "registry.opentofu.org/ahrzb/gws";
      description = "Provider source address; must match how the plugin is installed.";
    };

    dataSourceName = mkOption {
      type = types.str;
      default = "all";
      description = "Name of the `gws_gmail_labels` data source this module declares.";
    };
  };

  config = lib.mkIf cfg.enable {
    terraform.required_providers.gws.source = cfg.sourceAddress;
    provider.gws = { };
    data.gws_gmail_labels.${cfg.dataSourceName} = { };
    resource.gws_gmail_filter = checked;
  };
}
