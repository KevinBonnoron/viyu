#!/bin/sh
# The dependency audits the CI runs (ci.yml when dependencies change, release.yml every week on the latest
# release): JavaScript packages at high severity and above, and Go modules. Runs inside the Nix devshell, from
# the repository root. govulncheck only needs the module graph and sources: the embedded UI directories just
# have to exist.
set -eu

for app in client website; do
  (cd "apps/$app" && bun install --frozen-lockfile && bun audit --audit-level=high)
done

for dist in apps/server/internal/ui/dist apps/desktop/dist; do
  mkdir -p "$dist"
  [ -e "$dist/index.html" ] || touch "$dist/index.html"
done
(cd apps/server && govulncheck -tags ocr ./...)
(cd apps/desktop && govulncheck ./...)
