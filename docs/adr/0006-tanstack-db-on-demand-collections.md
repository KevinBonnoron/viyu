# ADR 0006: TanStack DB on-demand collections with infinite scroll

- Status: accepted
- Scope: `apps/client/src/collections`, `apps/client/src/features/video/collections` and every list in the UI

## Context

The `videos` and `channels` collections hold everything the server ever cached, far more than one account will view. Loading
them in full (`getFullList`) or mirroring them in client state is not viable. The UI still needs live updates (a channel
avatar arrives, a background job adds videos) and consistent optimistic writes for history, reactions and subscriptions.

## Decision

- Every PocketBase-backed collection is a TanStack DB collection built with `pocketbase-db-collection` and
  `syncMode: 'on-demand'`. The one exception is the one-row-per-user settings collection, which is small and loaded eagerly.
- Components read through live queries. Lists use `useLiveInfiniteQuery` with an `orderBy` and a page size and load the next
  page when the end of the virtualised grid scrolls into view. Single records are point queries on `id`.
  The query's `where`, `orderBy` and `limit` are compiled into PocketBase filters, so only the visible subset is fetched.
- Writes go through `collection.insert/update/delete`, with new ids from `collection.utils.newId()`. There are no direct
  `pb.collection(...).create/update/delete` calls and no hand-written requests to `/api/collections/*`.
- Queries are designed around the adapter's current limits: realtime upserts every event even outside the loaded subsets,
  pages refetch from the first page with a growing limit, and `inArray` compiles to an OR chain. Lists passed to `inArray`
  stay bounded by the page size; the subscriptions timeline slices channel ids (at most 40 per slice) and merges the slices.

## Consequences

- Memory and bandwidth follow what is on screen, not the size of the cache.
- Realtime updates reach every live query that matches, without hand-written subscriptions.
- The adapter limits shape the queries; a new list must respect them (bounded `inArray`, newest-first ordering), and moving to
  an upstream version that lifts them may allow simpler queries.
- Infinite scroll means totals are only exact once every page is loaded; the subscriptions timeline, for example, shows its
  count only then.
- Optimistic writes roll back when the server refuses them, so server rules and client queries must agree on the key of a
  row (history rows are identified by profile and video).
