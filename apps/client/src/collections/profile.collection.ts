import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb, pbStringCollation } from '@/lib/pocketbase';
import type { ProfileRecord } from '@/types';

// The account's own profiles; the server rule already restricts the collection to them.
export const profileCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<ProfileRecord>('profiles'),
    syncMode: 'on-demand',
    defaultStringCollation: pbStringCollation,
  }),
);
