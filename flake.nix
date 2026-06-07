{
  description = "winerank — YouTube transcript fetcher + wine shop scrapers";

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

        # cd to git root before running scripts so Path.cwd()-based paths resolve correctly.
        cdRoot = ''
          root=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
          cd "$root"
        '';

        # Build fetch + parse + combined scrape packages for a standard scraper.
        # fetchArgs: extra fixed args forwarded to fetch.py (before "$@")
        # outputPath: --output value passed to parse.py
        mkScraper = { name, fetchScript, parseScript, fetchArgs ? "", outputPath }:
          let
            fetch = pkgs.writeShellApplication {
              name = "fetch-${name}";
              runtimeInputs = [ pythonEnvScraper pkgs.git ];
              text = ''
                ${cdRoot}
                exec python3 ${fetchScript} ${fetchArgs} "$@"
              '';
            };
            parse = pkgs.writeShellApplication {
              name = "parse-${name}";
              runtimeInputs = [ pythonEnvScraper pkgs.git ];
              text = ''
                ${cdRoot}
                exec python3 ${parseScript} --from-cache --output ${outputPath}
              '';
            };
            scrape = pkgs.writeShellApplication {
              name = "scrape-${name}";
              runtimeInputs = [ pythonEnvScraper pkgs.git ];
              text = ''
                ${cdRoot}
                python3 ${fetchScript} ${fetchArgs} "$@"
                python3 ${parseScript} --from-cache --output ${outputPath}
              '';
            };
          in { inherit fetch parse scrape; };

        # Standard scrapers: one directory per merchant, wines.json output.
        standardScrapers = builtins.listToAttrs (map (name:
          {
            inherit name;
            value = mkScraper {
              inherit name;
              fetchScript = ./scraping/${name}/fetch.py;
              parseScript = ./scraping/${name}/parse.py;
              outputPath = "scraping/${name}/wines.json";
            };
          }
        ) [
          "arvi"
          "bauraulac"
          "flaschenpost"
          "gerstl"
          "landolt"
          "moevenpick"
          "more-than-wine"
          "passeur"
          "realwines"
          "rebwein"
          "smith-and-smith"
          "vinazion"
          "zweifel"
        ]);

        # Shopify scrapers: one generic script, two merchants.
        shopifyScrapers = builtins.listToAttrs (map ({ name, shop }:
          {
            inherit name;
            value = mkScraper {
              inherit name;
              fetchScript = ./scraping/shopify/fetch.py;
              parseScript = ./scraping/shopify/parse.py;
              fetchArgs = "--shop ${shop} --name ${name}";
              outputPath = "scraping/shopify/${name}.json";
            };
          }
        ) [
          { name = "vergani";    shop = "www.vergani.ch"; }
          { name = "advanvinum"; shop = "advanvinum-wein.ch"; }
        ]);

        allScrapers = standardScrapers // shopifyScrapers;

        # All scrape-* binaries as a list.
        allScrapePackages = map (name: allScrapers.${name}.scrape) (builtins.attrNames allScrapers);

        # combine-wines: merge all per-merchant wines.json into wines.json at repo root.
        combine-wines = pkgs.writeShellApplication {
          name = "combine-wines";
          runtimeInputs = [ pythonEnvScraper pkgs.git ];
          text = ''
            ${cdRoot}
            exec python3 ${./scraping/combine.py} "$@"
          '';
        };

        # scrape-all: run all merchants in parallel, combine into wines.json, print summary.
        scrape-all = pkgs.writeShellApplication {
          name = "scrape-all";
          runtimeInputs = allScrapePackages ++ [ combine-wines pkgs.git ];
          text = ''
            root=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
            cd "$root"

            merchants=(
              arvi bauraulac flaschenpost gerstl landolt moevenpick
              more-than-wine passeur realwines rebwein smith-and-smith
              vinazion zweifel vergani advanvinum
            )

            declare -A pids
            declare -A tmpfiles

            printf '\033[1mStarting %d scrapers in parallel...\033[0m\n' "''${#merchants[@]}"
            for m in "''${merchants[@]}"; do
              tmp=$(mktemp)
              tmpfiles[$m]=$tmp
              "scrape-$m" "$@" >"$tmp" 2>&1 &
              pids[$m]=$!
              printf '  [pid %d] %s\n' "''${pids[$m]}" "$m"
            done

            declare -a passed=()
            declare -a failed=()

            # Build a reverse map pid→merchant so we can identify who finished.
            declare -A pid_to_merchant
            for m in "''${merchants[@]}"; do
              pid_to_merchant[''${pids[$m]}]=$m
            done

            remaining=''${#merchants[@]}
            while [ "$remaining" -gt 0 ]; do
              if wait -n -p finished_pid; then
                code=0
              else
                code=$?
              fi
              m=''${pid_to_merchant[$finished_pid]}
              printf '\n\033[1m--- %s ---\033[0m\n' "$m"
              cat "''${tmpfiles[$m]}"
              rm -f "''${tmpfiles[$m]}"
              if [ $code -eq 0 ]; then
                passed+=("$m")
              else
                failed+=("$m")
              fi
              remaining=$(( remaining - 1 ))
            done

            printf '\n\033[1m--- Summary ---\033[0m\n'
            for m in "''${passed[@]}"; do printf '  \033[32mok\033[0m  %s\n' "$m"; done
            for m in "''${failed[@]}"; do printf '  \033[31mFAIL\033[0m %s\n' "$m"; done

            if [ ''${#failed[@]} -gt 0 ]; then
              printf '\n%d/%d merchants failed.\n' "''${#failed[@]}" "''${#merchants[@]}"
              exit 1
            fi

            printf '\n\033[1m==> combine\033[0m\n'
            combine-wines
            printf '\nAll %d merchants scraped and combined into wines.json.\n' "''${#merchants[@]}"
          '';
        };

        # Flatten all individual packages into a single attrset.
        scraperPackages = builtins.foldl'
          (acc: name: acc // {
            "fetch-${name}" = allScrapers.${name}.fetch;
            "parse-${name}" = allScrapers.${name}.parse;
            "scrape-${name}" = allScrapers.${name}.scrape;
          })
          {}
          (builtins.attrNames allScrapers);

      in
      {
        packages = scraperPackages // {
          inherit scrape-all combine-wines;

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

          default = self.packages.${system}.get-transcripts;
        };

        devShells.default = pkgs.mkShell {
          inputsFrom = [ self.packages.${system}.get-transcripts ];
          packages = allScrapePackages ++ [ scrape-all combine-wines ];
        };
      }
    );
}
