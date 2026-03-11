import type { SearchPage } from '@/types';
import { pb } from './pocketbase';

let handoff: { query: string; platform: string; source?: string; token: string; expires: number; page: SearchPage } | undefined;

export function handoffSearch(query: string, platform: string, source: string | undefined, page: SearchPage) {
  handoff = { query, platform, source, token: pb.authStore.token, expires: Date.now() + 30_000, page };
}

export function getSearchHandoff(query: string, platform: string, source?: string): SearchPage | undefined {
  const value = handoff;
  return value &&
    value.query === query &&
    value.platform === platform &&
    value.source === source &&
    value.token === pb.authStore.token &&
    value.expires > Date.now()
    ? value.page
    : undefined;
}
