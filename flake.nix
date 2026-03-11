{
  description = "Viyu development environment";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # Keep network-facing media tools current without replacing the native UI SDK.
  inputs.media.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs, media }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
      mediaPkgs = media.legacyPackages.${system};
      tesseract = pkgs.tesseract.override { enableLanguages = [ "eng" ]; };
      # Go 1.27.4 fixes GO-2026-6859 (net/http); nixpkgs still ships 1.27.3. Drop this override once it catches up.
      go = pkgs.go_1_27.overrideAttrs (old: rec {
        version = "1.27.4";
        src = pkgs.fetchurl {
          url = "https://go.dev/dl/go${version}.src.tar.gz";
          hash = "sha256-c8+4VxrsDAEXdofbhxY9Rqog6XeWcncokaoXZU2aYJk=";
        };
      });
      # Biome at the exact version biome.json is written for, from its official static build: nixpkgs lags behind, and
      # one binary serves Task, lefthook and CI alike.
      biome = pkgs.stdenvNoCC.mkDerivation rec {
        pname = "biome";
        version = "2.6.7";
        src = pkgs.fetchurl {
          url = "https://github.com/biomejs/biome/releases/download/%40biomejs%2Fbiome%40${version}/biome-linux-x64-musl";
          hash = "sha256-/nC0ZujXcUEx1NhtNXOqeV/gIj27RxGO79xuXHr9Ob4=";
        };
        dontUnpack = true;
        installPhase = "install -Dm755 $src $out/bin/biome";
      };
      # Tesseract and Leptonica are only linked into builds made with the `ocr` tag (`go build -tags ocr`);
      # a default build has no CGO dependency on them.
      ocrLibs = [ tesseract pkgs.leptonica ];
      # Wails v3 builds Linux against GTK4 + WebKitGTK 6.0 by default (no `gtk3` tag).
      desktopLibs = with pkgs; [
        gtk4 webkitgtk_6_0 glib glib-networking libsoup_3
        cairo pango gdk-pixbuf graphene dbus
        libx11 libxcomposite libxdamage libxext libxfixes libxrandr
        mesa libglvnd
        gst_all_1.gstreamer gst_all_1.gst-plugins-base gst_all_1.gst-plugins-good
        gst_all_1.gst-plugins-bad gst_all_1.gst-libav
      ];
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = with pkgs; [
          bun
          biome
          go
          gopls
          go-task
          lefthook
          govulncheck
          watchexec
          pkg-config
          gcc
          mailpit
          ffmpeg
          mediaPkgs.yt-dlp
          mediaPkgs.deno
        ] ++ desktopLibs ++ ocrLibs;

        CGO_ENABLED = "1";
        CGO_CPPFLAGS = "-I${tesseract}/include -I${pkgs.leptonica}/include";
        CGO_LDFLAGS = "-L${tesseract}/lib -L${pkgs.leptonica}/lib -Wl,-rpath,${pkgs.lib.makeLibraryPath (ocrLibs ++ [ pkgs.stdenv.cc.cc.lib ])}";
        TESSDATA_PREFIX = "${tesseract}/share/tessdata";
        LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath (desktopLibs ++ ocrLibs ++ [ pkgs.stdenv.cc.cc.lib ]);
        GIO_MODULE_DIR = "${pkgs.glib-networking}/lib/gio/modules";
        GIO_EXTRA_MODULES = "${pkgs.glib-networking}/lib/gio/modules";
        LIBGL_DRIVERS_PATH = "${pkgs.mesa}/lib/dri";
        __EGL_VENDOR_LIBRARY_FILENAMES = "${pkgs.mesa}/share/glvnd/egl_vendor.d/50_mesa.json";
        GST_PLUGIN_PATH = pkgs.lib.makeSearchPath "lib/gstreamer-1.0" (with pkgs.gst_all_1; [
          gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad gst-libav
        ]);

        shellHook = ''
          # CI installs dependencies explicitly (frozen lockfile) and never needs git hooks. Each JavaScript app has
          # its own package.json and lockfile; there is no workspace at the root.
          if [ -z "$CI" ]; then
            for app in client website; do
              if [ -f "$PWD/apps/$app/package.json" ] && [ ! -d "$PWD/apps/$app/node_modules" ]; then
                echo "→ Installing apps/$app dependencies..."
                (cd "$PWD/apps/$app" && bun install)
              fi
            done
          fi
          if [ -z "$CI" ] && [ -d "$PWD/.git" ] && [ ! -f "$PWD/.git/hooks/pre-commit" ]; then
            lefthook install
          fi
        '';
      };
    };
}
