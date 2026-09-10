{
  description = "OpenTofu/Terraform provider for Google Workspace (Gmail filters and labels)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "0.2.0";

        sourceAddress = "registry.opentofu.org/ahrzb/gws";

        provider = pkgs.buildGoModule {
          pname = "terraform-provider-gws";
          inherit version;
          src = ./.;
          vendorHash = "sha256-QCPaYhKQxa7lmCtHahg5PvoPf5d9FFQ+8ZV60XQY15U=";

          subPackages = [ "." ];
          # Providers distributed through the registry are built by goreleaser with cgo off;
          # matching that keeps the binary static and the closure small.
          env.CGO_ENABLED = 0;

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          # `withPlugins` does not look in $out/bin - it expects the exact layout nixpkgs'
          # own mkProvider produces, keyed by the source address and the target platform:
          #   libexec/terraform-providers/<address>/<version>/<goos>_<goarch>/terraform-provider-<name>_<version>
          # Leaving the binary in bin/ makes tofu report the plugin directory as missing.
          postInstall = ''
            dir=$out/libexec/terraform-providers/${sourceAddress}/${version}/''${GOOS}_''${GOARCH}
            mkdir -p "$dir"
            mv $out/bin/terraform-provider-gws "$dir/terraform-provider-gws_${version}"
            rmdir $out/bin
          '';

          # `terraform.withPlugins` / `opentofu.withPlugins` in nixpkgs key the plugin
          # directory off this attribute, and OpenTofu resolves `source` in required_providers
          # against it. It has to match the Address in main.go, or tofu ignores the plugin and
          # tries to reach a registry that has never heard of it.
          passthru.provider-source-address = sourceAddress;

          meta = {
            description = "Declarative Google Workspace resources for OpenTofu and Terraform";
            homepage = "https://github.com/ahrzb/terraform-provider-gws";
            license = pkgs.lib.licenses.mit;
          };
        };
      in
      {
        packages = {
          default = provider;
          terraform-provider-gws = provider;

          # An OpenTofu with this provider already installed, for trying it out without
          # writing a plugin block: `nix run .#tofu -- plan`.
          tofu = pkgs.opentofu.withPlugins (_: [ provider ]);
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gcc # cgo is needed to build the test binaries
            pkgs.opentofu
            pkgs.gofumpt
          ];
        };

        checks = {
          build = provider;
          gotest = provider.overrideAttrs (old: {
            name = "terraform-provider-gws-tests";
            doCheck = true;
          });
        };

        formatter = pkgs.nixfmt-tree;
      }
    )
    // {
      # For consumers: adds `terraform-provider-gws` to pkgs, so an existing
      # `opentofu.withPlugins` call can pick it up alongside the nixpkgs providers.
      overlays.default = final: _prev: {
        terraform-provider-gws = self.packages.${final.stdenv.hostPlatform.system}.terraform-provider-gws;
      };

      # A typed terranix schema for filters, so consumers write `archive = true` instead of
      # repeating label lookups and add/remove mechanics at every call site. It ships here
      # rather than in the consumer because it is knowledge about *this provider*, and it
      # carries the sender-conflict assertion that the resources themselves cannot express.
      terranixModules = rec {
        gmail = ./modules/terranix/gmail.nix;
        default = gmail;
      };
    };
}
