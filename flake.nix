{
  description = "winerank — YouTube transcript fetcher + realwines.ch scraper";

  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };

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

      in
      {
        packages = rec {
          inherit fetch-realwines parse-realwines scrape-realwines;

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

          extract-scores = pkgs.buildGoModule {
            pname = "extract-scores";
            version = "0.1.0";
            src = ./cmd/extract-scores;
            vendorHash = null;
          };

          eval = pkgs.buildGoModule {
            pname = "eval";
            version = "0.1.0";
            src = ./cmd/eval;
            vendorHash = null;
          };
          default = get-transcripts;
        };

        devShells.default = pkgs.mkShell {
          inputsFrom = [ self.packages.${system}.get-transcripts ];
          # llama-vulkan is also a viable alternative that doesn't require
          # building from source. With that version I get perf in the region of
          # 75 tokens/s. I think CUDA will be faster.
          packages = with pkgs; [ (llama-cpp.override { cudaSupport = true; }) ];
        };
      }
    );
}
