# ADR 0002: PocketBase as the backend, with one schema snapshot while pre-alpha

- Status: accepted
- Scope: `apps/server`, `apps/server/internal/migrations`

## Context

Viyu needs accounts, per-user data with access rules, a realtime feed of record changes, file storage for avatars and an
administration surface. It is self-hosted by individuals, so it must ship as one process with one embedded database and no
extra services.

The project is pre-alpha. There are no deployed installations whose data must survive, and carrying a migration per past
schema change would only preserve history nobody needs.

## Decision

- The server is a Go program that embeds PocketBase (SQLite, auth, collection rules, realtime, dashboard) and adds its own
  routes (`/api/search`, `/api/stream/…` and the rest) next to PocketBase's. The web UI is embedded in the same binary and
  served from the same origin.
- Per-user data is protected by PocketBase collection rules (`profile.user`, `owners`, `@request.auth.id`), not by ad-hoc
  checks in each handler. Writes that need server logic go through custom routes or hooks.
- The complete schema is one file, `apps/server/internal/migrations/schema.json`. An initial migration creates a fresh
  database from it; a sync migration named after the snapshot's hash (and a version of the sync code) brings an existing
  database to it once: collections upserted by stable id, fields matched by name, retyped fields converted with their
  values, fields the snapshot no longer has dropped. Automatic migration generation is off.
- A rename is not a sync operation (it would drop the old field's values): it needs the new field and a data copy in the
  same change, or a stable field id kept across the rename. Backups are the operator's responsibility
  ([ADR 0001](0001-pocketbase-field-order.md) keeps the snapshot readable).

## Consequences

- One binary, one volume: deployment is a container with `/app/pb_data`. Backups are a directory copy.
- The schema is reviewed as one document, and the field-order test of ADR 0001 covers all of it.
- Schema changes reach existing databases without a reset. Data that only a removed field held is lost by design, so
  removing or renaming a field is reviewed for what it drops.
- Viyu follows PocketBase's release cadence and its Go API (currently the `ganigeorgiev.com/pocketbase` module path,
  v0.41); upgrades are dependency bumps that need the test suite.
- SQLite limits the server to a single writer node; horizontal scaling is out of scope.
