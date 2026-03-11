import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb, pbStringCollation } from '@/lib/pocketbase';
import type { SubscriptionRecord } from '@/types';

// A subscription carries its channel and creator (`expand.channel`, `expand.creator`), so listing subscriptions never loads the channels or creators collections.
export const subscriptionCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<SubscriptionRecord>('subscriptions'),
    syncMode: 'on-demand',
    defaultStringCollation: pbStringCollation,
    options: { expand: 'channel,creator' },
  }),
);
