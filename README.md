# Viyu

Self-hosted video subscriptions and player for YouTube, Twitch and PeerTube.

A single Go process runs the video API, [PocketBase](https://pocketbase.io) (accounts, storage, realtime) and the embedded
React web UI. yt-dlp does the extraction. A Linux desktop app (Wails 3) and an Android app (Capacitor) are thin clients of
a Viyu server you choose.

> **Status: pre-alpha.** The database schema is one snapshot; a running instance is brought up to it at startup, data
> kept (see [Data and backups](#data-and-backups)). Expect rough edges.

## Features

- Subscribe to YouTube, Twitch and PeerTube channels; one feed, one subscriptions timeline, history, likes and "not interested".
- Accounts with several profiles; each profile has its own subscriptions, history, reactions, home feed and accent colour. Subscriptions can be moved from one profile to another (to split a catch-all profile), creator groups travelling whole.
- Own HLS player: quality, audio language, speed, chapters, resume, SponsorBlock skipping, private playback.
- Live streams: LIVE badges, jump-to-live, read-only Twitch chat (live and VOD replay).
- Series inferred from YouTube and Twitch channel catalogs (and from Twitch collections), with seasons and next-episode playback.
- Search across the sources you enabled; paste a video link to open it.
- New-video notifications, watch statistics, account export/import as JSON.
- Optional connected sources (cookie import) for personalised YouTube/Twitch feeds and watch marking.
- Per-account source switches; PeerTube is off until you turn it on.
- Administration: public registration switch, user and role management, thumbnail OCR switch.
- English and French interface; light, dark or system theme.

The [user guide](https://kevinbonnoron.github.io/viyu/) describes each of them.

## Quick start

### Docker

```yaml
# compose.yaml
services:
  viyu:
    image: ghcr.io/kevinbonnoron/viyu:latest
    ports:
      - "8090:8090"
    environment:
      PB_SUPERUSER_EMAIL: admin@example.com
      PB_SUPERUSER_PASSWORD: change-me-please
    volumes:
      - viyu-data:/app/pb_data
    restart: unless-stopped

volumes:
  viyu-data:
```

```sh
docker compose up -d
```

Open <http://localhost:8090>, register an account, and give it the application `admin` role (the PocketBase dashboard at
`/_/` is a separate superuser account):

```sh
docker compose stop viyu
docker compose run --rm viyu admin promote you@example.com --dir=/app/pb_data
docker compose start viyu
```

The image runs one non-root process (UID/GID `1000:1000`) with yt-dlp, FFmpeg and Deno, exposes port 8090, stores data in
`/app/pb_data` and has a `HEALTHCHECK` (`viyu healthcheck`). A bind mount instead of a named volume must be writable by
`1000:1000`. Put the server behind HTTPS before using it from the desktop or Android app.

`PB_SUPERUSER_*` only create the first dashboard superuser when none exists and may be removed afterwards. Passwords
change later in the dashboard or with `viyu superuser upsert EMAIL PASSWORD`.

### Development (Nix devshell)

Every command runs inside the devshell, which provides Go, Bun, Task, yt-dlp, FFmpeg, Deno, the GTK/WebKit libraries for the
desktop app and the Tesseract/Leptonica libraries for OCR.

```sh
nix develop
task install
task dev            # Go + PocketBase on 127.0.0.1:8090, Vite on http://localhost:5173
```

Vite waits for `/api/health` (up to two minutes), then proxies `/api` and `/_` to the Go server. Run `task` to list all tasks.

## Configuration

Environment variables read by the code. The standalone `viyu` executable only reads the process environment; Docker
Compose also reads a `.env` file next to `compose.yaml` for substitution.

| Variable                                      | Used by            | Default                                                                    | Purpose                                                                               |
| --------------------------------------------- | ------------------ | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `VIYU_DATA_DIR`                               | server             | `db/pb_data`                                                               | Default data directory. The `--dir` flag takes precedence.                            |
| `VIYU_UI_DIR`                                 | server             | embedded UI                                                                | Serve the web UI from this directory instead of the embedded build.                   |
| `PB_SUPERUSER_EMAIL`, `PB_SUPERUSER_PASSWORD` | server             | unset                                                                      | Create the first dashboard superuser when both are set and none exists.               |
| `VIYU_SOURCE_KEY_FILE`                        | server             | `<data dir>/source_connections.key`                                        | Absolute path of the key that encrypts connected-source cookies.                      |
| `VIYU_YTDLP_CONCURRENCY`                      | server             | `6`                                                                        | Maximum yt-dlp processes running at once, for requests and background jobs together.  |
| `VIYU_PEERTUBE_INSTANCES`                     | server             | `framatube.org, video.blender.org, tilvids.com, diode.zone, makertube.net` | Comma-separated PeerTube hosts the server may contact, read once at startup.          |
| `VIYU_HEALTHCHECK_URL`                        | `viyu healthcheck` | `http://127.0.0.1:8090/api/health`                                         | Probe address when the server listens elsewhere than the image default.               |
| `VIYU_SERVER_URL`                             | Vite dev server    | `http://127.0.0.1:8090`                                                    | Backend the dev server proxies to.                                                    |
| `VITE_CLIENT_TARGET`                          | client build       | unset                                                                      | `desktop` builds the shell UI with the server-setup screen (`task build:ui:desktop`). |

`TESSDATA_PREFIX` is read by Tesseract itself (the image and devshell set it). Docker builds accept the `YT_DLP_VERSION`
build argument; CI rebuilds and republishes the image weekly with the latest yt-dlp so extractor fixes arrive without a release.

Server flags are PocketBase's: `viyu serve --http=127.0.0.1:8090 --dir=db/pb_data`.

## Security and privacy

- **An account is required for everything that reaches the network.** Search, link resolution, video and channel data,
  live status, chat and media streams need a signed-in user. Only `/api/health`, `/api/registration` and PocketBase's auth
  endpoints are anonymous. Sessions travel as `Authorization: Bearer` headers (no cookie), including HLS and live-chat requests.
- **Public registration** is on by default; administrators can switch it off. Roles cannot be changed through self-service;
  the last administrator cannot be removed or demoted.
- **Rate limits** are on (PocketBase's limiter): 10 auth requests, 60 creations, 600 API calls and 1500 stream requests per 10 s.
- **SSRF hardening.** The server's own HTTP fetches (PeerTube, Twitch, publication dates, thumbnail OCR) go through a client
  that refuses private, loopback and link-local addresses, and PeerTube is limited to the instances you list.
- **Connected sources store session cookies on the server.** They are encrypted with AES-256-GCM, bound to the user and
  platform, and kept in a field no API client can read. The key lives in `source_connections.key` (mode 0600) in the data
  directory, or at `VIYU_SOURCE_KEY_FILE`. yt-dlp receives a decrypted temporary file in a private directory, removed after
  use. The server operator can read your session by design: connect only to a server you trust.
- **Terms-of-service risk.** Using a logged-in YouTube or Twitch session through automated tooling may violate their terms
  and can get the account restricted or banned. Viyu cannot prevent that; prefer a secondary account.
- **Owners privacy.** Videos and channels fetched through a connected source belong to the accounts that fetched them
  (`owners`) and are invisible to others. A fetch without a connected source makes the record public.
- **Third parties contacted by the browser** (allowed by the Content-Security-Policy, nothing else is):
  [SponsorBlock](https://sponsor.ajay.app) receives the YouTube video id for skip segments;
  [Return YouTube Dislike](https://returnyoutubedislike.com) receives the id when YouTube hides the dislike count;
  artwork and emotes load from `i.ytimg.com`, `static-cdn.jtvnw.net` and the PeerTube instance hosts. Cookies and account
  tokens are never sent to them.
- The Android app disables backups (`allowBackup="false"`).

## Native clients

Both clients embed only the UI and ask for your server's address (`http://` or `https://`) on first launch.
They do not run PocketBase, yt-dlp or OCR. Authentication is stored per server; **Change server** is in the account menu.

- **Desktop (Linux only).** `task desktop:run` or `task desktop:package` (a `bin/viyu-desktop-linux-<arch>.tar.gz` with the
  executable and `.desktop` launcher). Needs GTK4 and WebKitGTK 6.0 on the host; no libraries are bundled, and binaries
  built in the Nix shell keep Nix runtime references. Not an AppImage; no Windows or macOS build.
- **Android.** CI publishes a debug-signed APK (`viyu-android-debug` artifact, min SDK 24) on release tags and manual runs,
  next to the Linux desktop and server binaries. There is no release signing.
  Locally: `bun run --cwd apps/client build:android`, then `./gradlew assembleDebug` in `apps/android`.

## Data and backups

The schema is a single snapshot (`apps/server/internal/migrations/schema.json`). A new data directory is created from it;
an existing one is synced to it once per snapshot version at startup: collections, fields, indexes and rules are brought to
match, records are kept, a field whose type changed keeps its values, and only the values of a field the snapshot no longer
defines are dropped. Back up before upgrading. Never run two processes against the same directory.

Back up the whole data directory (`pb_data`: `data.db`, `auxiliary.db`, `storage/`) **and** the encryption key. Without
the key, saved sources must be reconnected. If you set `VIYU_SOURCE_KEY_FILE`, back that file up separately; keeping it off the
backup medium means a stolen data copy does not hold both ciphertext and key. Never publish cookie exports, the key or backups.

## Logs

Warnings and errors (a platform that failed a search, a rate limit, a background job that failed) are printed to standard
error, so `docker compose logs` shows them, and are also kept in the PocketBase dashboard (`/_/`, **Logs**) with the
informational entries.

## Development tasks

Run inside `nix develop`.

| Task                                                                                                         | What it does                                                                                       |
| ------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| `task install`                                                                                               | Install Bun dependencies and Go modules.                                                           |
| `task dev`                                                                                                   | Run `dev:server` (watchexec + Go) and `dev:client` (Vite). `dev:docs` serves this documentation.   |
| `task build:ui` / `task build:ui:desktop`                                                                    | Build the web UI into the server embed directory / the desktop UI into `apps/desktop/dist`.        |
| `task build`                                                                                                 | Build `bin/viyu` with the embedded UI.                                                             |
| `task desktop:build`, `desktop:run`, `desktop:package`                                                       | Build, launch or package the Linux desktop app.                                                    |
| `task test`                                                                                                  | Server tests with `-race`, desktop and cookie-helper tests, client tests.                          |
| `task lint`                                                                                                  | Biome, version check, `gofmt`, `go vet`.                                                           |
| `task type-check`, `task format`, `task audit`, `task version:check`, `task build:docs`, `task docker:build` | Client types, formatting, vulnerability scan, version consistency, docs build, local Docker image. |

The version lives in the root `package.json` only. `task version:check` keeps `apps/client/package.json` in step, and the
Android version and the Docker image label derive from it. A lefthook pre-commit hook (Biome, gofmt) is installed by the
devshell; skip it once with `LEFTHOOK=0`. Dependabot proposes weekly updates for Go, Bun, Docker, GitHub Actions and Gradle.

OCR for series numbering needs Tesseract and Leptonica and is compiled in with the `ocr` build tag (`go build -tags ocr`).
The Docker image builds with it and the devshell provides the libraries. Without the tag the server builds without CGO and
reports OCR as unavailable.

Stack: Go 1.27, PocketBase 0.41, Wails 3 (`v3.1.0`, GTK4 build), React 19 with TanStack Router/DB, Vite, Bun, VitePress.

## Layout

- `apps/server`: Go + PocketBase server (`main.go`: `serve`, `superuser`, `admin promote`, `healthcheck`).
  - `internal/app`: composition only (options, wiring, PocketBase hooks, start/shutdown).
  - `internal/httpapi`: thin route handlers, auth middleware, one `{code, message}` error envelope (`internal/apierr`).
  - `internal/platform`: platform interface and injected registry; `youtube/`, `twitch/`, `peertube/` hold every
    platform-specific rule, and `all/` is the one list of them (the schema's `platform` selects are tested against it).
  - `internal/ytdlp`: the single yt-dlp runner (global concurrency budget, process-group kill, typed errors).
  - `internal/safehttp`: outbound HTTP client that refuses private/reserved addresses, redirects included.
  - `internal/cache`: generic TTL cache with single-flight.
  - `internal/catalog`: video/channel records (upsert, `owners` privacy, presentation, history identity).
  - `internal/media`: HLS proxy (manifest cache, playlist rewrite, resource type whitelist).
  - `internal/sources`: connected sessions (encrypted cookies) and per-account source activation.
  - `internal/accounts`, `subscriptions`, `feed`, `search`, `stats`, `comments`, `live`, `chat`: features.
  - `internal/jobs`: background queues and workers (catalog sync, metadata and profile backfills).
  - `internal/series`: series inference and storage; `series/ocr` is compiled only with `-tags ocr`.
  - `internal/migrations`: the single schema snapshot (`schema.json`); `internal/ui`: embedded UI and its headers.
- `apps/client`: React UI shared by web, desktop and Android ([notes](apps/client/README.md)).
- `apps/desktop`: Wails 3 shell and Linux packaging.
- `apps/android`: Capacitor Android project.
- `apps/website`: VitePress user guide, published to GitHub Pages.
- `docs/adr`: architecture decision records.
- `tools/youtube-cookie-helper`: experimental helper that copies YouTube cookies from a local browser profile to the clipboard.
- `docker/Dockerfile`, `flake.nix`, `Taskfile.yml`, `.github/workflows/ci.yml`: build, toolchain and CI.

CI runs only the jobs the changed paths need (the table is at the top of `ci.yml`): a client change builds and tests the UI,
a Dockerfile change builds the image for amd64 without pushing, and so on. Dependency audits (`bun audit`, `govulncheck`) run
when dependencies change, on main and weekly, so a newly published advisory does not fail an unrelated pull request.

## License

MIT. See [LICENSE](LICENSE).
