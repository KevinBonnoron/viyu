import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb } from '@/lib/pocketbase';
import type { VideoRecord } from '@/types';

// On-demand: a live query compiles its clauses into a PocketBase filter, so the (huge, shared) cache of videos is never loaded whole.
export const videoCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<VideoRecord>('videos'),
    syncMode: 'on-demand',
  }),
);
