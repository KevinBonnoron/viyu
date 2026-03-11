# ADR 0001: Field order in PocketBase collections

- Status: accepted
- Scope: every base collection of the Viyu server (`apps/server/internal/migrations`)

## Context

PocketBase keeps the fields of a collection in an ordered list. That order is what the admin dashboard shows and what a developer reads first when opening a collection. It carries no meaning for the database itself.

The schema was once written with a consistent order, but the migrations that came later (`Fields.Add`) appended each new field at the end of the list, so collections ended with `created`, `updated` and then the newest fields. Nothing noticed it because the only order check ran on the initial schema.

## Decision

The fields of a collection appear in this order, with no exception:

1. `id`
2. the ordinary fields, which are every field that is not a relation, in the order that groups them best (identity first, then content, then state and cache bookkeeping)
3. the relation fields
4. `created`
5. `updated`

This applies to base collections. Auth collections keep PocketBase's own system fields first.

`schema.json` is the single, complete schema (there is one initial migration), so a new field is added to `schema.json` in this position. A test loads the schema and fails when a collection does not follow the order. Any new collection follows the same order.

## Consequences

- Reading a collection, in the dashboard or in `schema.json`, shows its identity first and its relations and timestamps always in the same place.
- Reordering is metadata only: columns, data, API responses and filters are untouched.
- Forgetting the order when adding a field is caught by the test rather than by a reader.
