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
          go_1_27
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
          # CI installs dependencies explicitly (frozen lockfile) and never needs git hooks.
          if [ -z "$CI" ] && [ ! -d "$PWD/node_modules" ]; then
            echo "→ Installing dependencies..."
            bun install
          fi
          if [ -z "$CI" ] && [ -d "$PWD/.git" ] && [ ! -f "$PWD/.git/hooks/pre-commit" ]; then
            lefthook install
          fi
        '';
      };
    };
}
