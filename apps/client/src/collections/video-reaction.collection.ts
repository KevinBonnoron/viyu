import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb } from '@/lib/pocketbase';
import type { VideoReactionRecord } from '@/types';

// A reaction carries its video and the video's channel, which is what the likes page renders from.
export const videoReactionCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<VideoReactionRecord>('video_reactions'),
    syncMode: 'on-demand',
    options: { expand: 'video,video.channel' },
  }),
);
