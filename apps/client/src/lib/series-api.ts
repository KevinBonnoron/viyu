import type { SeriesDetailResponse, SeriesResponse } from '@/types/series.type';
import { request } from './api';
import { path } from './api-request';

interface Options {
  signal?: AbortSignal;
}

export const seriesApi = {
  list: (profileId: string, { signal }: Options = {}) => request<SeriesResponse>('/series', { query: { profile: profileId }, signal, cache: 'no-store' }),
  detail: (seriesId: string, profileId: string, { signal }: Options = {}) =>
    request<SeriesDetailResponse>(path`/series/${seriesId}`, { query: { profile: profileId }, signal, cache: 'no-store' }),
};
