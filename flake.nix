{
  description = "winerank — YouTube transcript fetcher + realwines.ch scraper";

  inputs = {
    # TODO: This is pinned to the same rev as ~/src/boxen to avoid re-downloading
    # nixpkgs on a slow connection. Replace with "github:nixos/nixpkgs/nixos-unstable"
    # and run `nix flake update` once you're on a faster connection.
    nixpkgs.url = "github:nixos/nixpkgs/ed67bc86e84e51d4a88e73c7fd36006dc876476f";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        pythonEnv = pkgs.python3.withPackages (ps: [
          ps.yt-dlp
          ps.youtube-transcript-api
        ]);

        pythonEnvScraper = pkgs.python3.withPackages (ps: [
          ps.requests
        ]);

        # Fetch all pages from the WooCommerce Store API into ./realwines/cache/
        fetch-realwines = pkgs.writeShellApplication {
          name = "fetch-realwines";
          runtimeInputs = [ pythonEnvScraper ];
          text = ''
            exec python3 ${./realwines/fetch.py} "$@"
          '';
        };

        # Parse cached pages into a clean wines.json
        parse-realwines = pkgs.writeShellApplication {
          name = "parse-realwines";
          runtimeInputs = [ pythonEnvScraper ];
          text = ''
            exec python3 ${./realwines/parse.py} "$@"
          '';
        };

        # Convenience: fetch (if needed) then parse → realwines/wines.json
        scrape-realwines = pkgs.writeShellApplication {
          name = "scrape-realwines";
          runtimeInputs = [ pythonEnvScraper ];
          text = ''
            set -euo pipefail
            SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
            CACHE_DIR="$SCRIPT_DIR/../realwines/cache"
            OUT="$SCRIPT_DIR/../realwines/wines.json"
            python3 ${./realwines/fetch.py} "$@" > /dev/null
            python3 ${./realwines/parse.py} --from-cache --output "$OUT"
            echo "Done → $OUT"
          '';
        };

        get-transcripts = pkgs.python3Packages.buildPythonApplication {
          pname = "get-transcripts";
          version = "0.1.0";
          pyproject = true;

          src = ./.;

          build-system = [
            pkgs.python3Packages.setuptools
          ];

          propagatedBuildInputs = [
            pkgs.python3Packages.yt-dlp
            pkgs.python3Packages.youtube-transcript-api
          ];
        };
      in
      {
        packages = {
          inherit get-transcripts fetch-realwines parse-realwines scrape-realwines;
          default = get-transcripts;
        };

        devShells.default = pkgs.mkShell {
          packages = [ pythonEnv ];
        };
      }
    );
}
