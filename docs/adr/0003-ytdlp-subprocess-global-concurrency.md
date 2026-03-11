# ADR 0003: yt-dlp as a subprocess with a global concurrency budget

- Status: accepted
- Scope: every server code path that extracts YouTube or Twitch data

## Context

YouTube and Twitch publish no stable public API for what Viyu needs (metadata, catalogs, comments, playable manifests).
yt-dlp tracks their changes within days. It is Python, needs FFmpeg and (for YouTube's challenge solver) Deno, and each
invocation costs hundreds of milliseconds and tens of megabytes. A burst of searches, page views and background jobs could
exhaust a small host if each started its own process.

## Decision

- yt-dlp is executed as an external process, never linked or embedded. The Docker image pins a yt-dlp release and CI rebuilds
  the image weekly with the latest one; the Nix devshell provides yt-dlp, FFmpeg and Deno.
- All invocations go through one runner. It holds a global budget of concurrent processes (default 6, overridden by
  `VIYU_YTDLP_CONCURRENCY`), shared by request handlers and background jobs. A caller waits for a slot or for its context.
- Each process is its own process group and is killed as a group when the request is cancelled or the server shuts down, so
  yt-dlp's FFmpeg and downloader children never outlive it. Standard error is read through a bounded buffer.
- A YouTube HTTP 429 pauses background metadata dispatch across channels and resumes gradually.
- Cookies for connected sources are passed as a short-lived file ([ADR 0004](0004-connected-source-cookies-server-side-encrypted.md)).

## Consequences

- Extractor breakage is fixed by updating yt-dlp, without releasing Viyu.
- Latency under load grows (callers queue) instead of the host running out of memory or process slots; operators tune the
  budget to their hardware.
- Background work competes with interactive requests for the same slots. Jobs must stay bounded and use cached results where
  they can.
- Extraction is only as available as yt-dlp: an outdated binary shows up as failed streams while metadata may still work.
- The image needs Python, FFmpeg and Deno at runtime and an init process (tini) to reap children.
