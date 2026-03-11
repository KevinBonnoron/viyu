/**
 * A cheap fingerprint of the live `series` records a server-computed answer (summaries, ordered episodes, sync state)
 * is derived from: it changes when a record appears, disappears or is updated, whatever the order they arrive in.
 * Comparing two keys is how a page knows its HTTP answer is stale, instead of subscribing to the records itself.
 */
export function seriesChangeKey(records: readonly { id: string; updated: string }[]): string {
  return records
    .map((record) => `${record.id}:${record.updated}`)
    .sort()
    .join(',');
}
