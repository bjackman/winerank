{
  description = "winerank — YouTube transcript fetcher for KonstantinBaumMasterOfWine";

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

        # Python environment with the transcript API library
        pythonEnv = pkgs.python3.withPackages (ps: [
          ps.youtube-transcript-api
        ]);

        # The fetch-transcripts app: wraps the Python script and puts
        # both yt-dlp and the Python env on PATH.
        fetch-transcripts = pkgs.writeShellApplication {
          name = "fetch-transcripts";
          runtimeInputs = [ pkgs.yt-dlp pythonEnv ];
          text = ''
            exec python3 ${./scripts/fetch_transcripts.py} "$@"
          '';
        };
      in
      {
        packages = {
          inherit fetch-transcripts;
          default = fetch-transcripts;
        };

        apps = {
          fetch-transcripts = flake-utils.lib.mkApp { drv = fetch-transcripts; };
          default = flake-utils.lib.mkApp { drv = fetch-transcripts; };
        };

        devShells.default = pkgs.mkShell {
          packages = [ pkgs.yt-dlp pythonEnv ];
          shellHook = ''
            echo "winerank dev shell ready."
            echo "Run: python3 scripts/fetch_transcripts.py --help"
          '';
        };
      }
    );
}
