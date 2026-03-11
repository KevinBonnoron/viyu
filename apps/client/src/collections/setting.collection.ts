import { createCollection } from '@tanstack/react-db';
import { pocketbaseCollectionOptions } from 'pocketbase-db-collection';
import { pb } from '@/lib/pocketbase';
import type { SettingsRecord } from '@/types';

// The one collection that is loaded whole: the server rule restricts it to the signed-in user's single settings row.
export const settingCollection = createCollection(
  pocketbaseCollectionOptions({
    recordService: pb.collection<SettingsRecord>('settings'),
  }),
);
