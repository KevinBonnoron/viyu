# ADR 0007: Thumbnail OCR is behind the `ocr` build tag

- Status: accepted
- Scope: server build, Docker image, CI

## Context

Series inference prefers episode numbers found in titles. For videos whose title carries none, an optional pass reads the
number from thumbnails with Tesseract through gosseract. That binding needs CGO, the Tesseract and Leptonica development
libraries at build time and their shared libraries plus trained data at runtime. It affects every build of the server, even
though most of it (tests, desktop, quick local runs) never needs OCR, and it prevents a plain `go build` or a static binary.

## Decision

- The OCR implementation is compiled only with `-tags ocr`. The default build has no CGO dependency and no gosseract; OCR
  reports itself as unavailable and the administration switch is hidden or reports the unavailable state.
- The Docker image builds the server with `-tags ocr` and installs the Tesseract runtime and a pinned English model, so the
  published server behaves as before. Checks run both with and without the tag.
- The decision to run OCR stays with the operator: when compiled in, administrators can switch it off, which cancels queued
  work and keeps cached results.
- The desktop and Android clients never contain OCR; they talk to a server.

## Consequences

- `go build ./...` and `go test ./...` work without Tesseract; contributors only need the OCR libraries (provided by the
  devshell) to work on the OCR code itself.
- There are two build configurations to keep green. Code behind the tag stays small and sits behind an interface so the
  rest of series inference does not depend on it.
- A server built without the tag silently lacks episode numbers that only OCR can find; documentation and the admin page
  must say so.
- The image is larger and the CGO build slower than a pure Go build, which is accepted for the published image.
