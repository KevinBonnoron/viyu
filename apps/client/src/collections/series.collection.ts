import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb } from '@/lib/pocketbase';
import type { SeriesRecord } from '@/types/series.type';

// On-demand: each live query compiles its clause into a PocketBase filter, so a channel page loads
// only its own series instead of every channel's.
export const seriesCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<SeriesRecord>('series'),
    syncMode: 'on-demand',
  }),
);
