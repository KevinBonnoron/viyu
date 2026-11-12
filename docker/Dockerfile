# syntax=docker/dockerfile:1
# UI_BUILD selects the UI source: `frontend` builds it here, `frontend-prebuilt` reuses a CI artifact in .ci/ui.
ARG UI_BUILD=frontend
# Release version recorded in the image labels (CI passes the root package.json version).
ARG VERSION=dev
# Explicit yt-dlp release. The weekly scheduled CI build overrides it with the latest PyPI release
# so a rebuilt image picks up YouTube extractor fixes; local builds stay reproducible.
ARG YT_DLP_VERSION=2026.10.26

# Base images are pinned by tag AND digest (dependabot's docker ecosystem bumps both).
FROM oven/bun:1.4.4@sha256:1d22548f177f4ff38e0c15baa3c2c46c6c2060ba2d1bc573bb4720b407548f1d AS frontend
WORKDIR /src
COPY package.json bun.lock tsconfig.json LICENSE ./
COPY apps/client/package.json ./apps/client/package.json
COPY apps/website/package.json ./apps/website/package.json
RUN bun install --frozen-lockfile
COPY apps/client ./apps/client
RUN bun run --cwd apps/client build && mkdir -p /src/client && cp -R apps/client/dist /src/client/dist

FROM scratch AS frontend-prebuilt
COPY .ci/ui/ /src/client/dist/

FROM ${UI_BUILD} AS ui

FROM golang:1.27-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS backend
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc g++ libc6-dev pkg-config libtesseract-dev libleptonica-dev \
    && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=1
WORKDIR /src/apps/server
COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download
COPY apps/server ./
COPY --from=ui /src/client/dist ./internal/ui/dist
# -tags ocr compiles the Tesseract thumbnail OCR in (CGO); the runtime stage ships its libraries.
RUN go build -tags ocr -trimpath -ldflags='-s -w' -o /viyu .

FROM denoland/deno:bin-2.9.8@sha256:b507583d3c68ba69f3ed2ce5969ae5b8b70f43421c781279c7aea06435263543 AS deno

FROM python:3.15-slim-bookworm@sha256:96599df822d0e1c6bb700014a05c7f3c8cabc6e0f7ff3c4f9f69cee033b4b5d9
ARG VERSION
ARG YT_DLP_VERSION
LABEL org.opencontainers.image.title="Viyu" \
      org.opencontainers.image.source="https://github.com/KevinBonnoron/viyu" \
      org.opencontainers.image.version="${VERSION}" \
      org.viyu.yt-dlp.version="${YT_DLP_VERSION}"
# tini runs as PID 1 so the yt-dlp/ffmpeg child processes spawned by the server are reaped.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg tini libtesseract5 liblept5 \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir "yt-dlp[default]==${YT_DLP_VERSION}" \
    && groupadd --gid 1000 viyu \
    && useradd --create-home --uid 1000 --gid viyu viyu \
    && mkdir -p /app/pb_data /usr/share/tesseract-ocr/5/tessdata && chown viyu:viyu /app/pb_data
ENV TESSDATA_PREFIX=/usr/share/tesseract-ocr/5/tessdata
# Debian's tesseract-ocr-eng contains only LSTM data; thumbnail OCR uses the legacy engine.
ADD --chmod=0644 --checksum=sha256:daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047 https://raw.githubusercontent.com/tesseract-ocr/tessdata/ced78752cc61322fb554c280d13360b35b8684e4/eng.traineddata /usr/share/tesseract-ocr/5/tessdata/eng.traineddata
COPY --from=backend /viyu /usr/local/bin/viyu
COPY --from=deno /deno /usr/local/bin/deno
USER 1000:1000
VOLUME /app/pb_data
EXPOSE 8090
# The probe is the server binary itself: no interpreter dependency, loopback never goes through
# HTTP(S)_PROXY. Set VIYU_HEALTHCHECK_URL when overriding --http.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/usr/local/bin/viyu", "healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/viyu"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/app/pb_data"]
