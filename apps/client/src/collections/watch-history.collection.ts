import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb } from '@/lib/pocketbase';
import type { WatchHistoryRecord } from '@/types';

// A history row carries its video and the video's channel, which is what the history page renders from.
export const watchHistoryCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<WatchHistoryRecord>('watch_history'),
    syncMode: 'on-demand',
    options: { expand: 'video,video.channel' },
  }),
);
