import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb, pbStringCollation } from '@/lib/pocketbase';
import type { ChannelRecord } from '@/types';

// On-demand, like the videos: only the channels a query asks for are loaded.
export const channelCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<ChannelRecord>('channels'),
    syncMode: 'on-demand',
    defaultStringCollation: pbStringCollation,
  }),
);
