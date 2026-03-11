import type { AdminUser, AdminUserInput, AdminUsersPage } from '@/types';
import { request } from './api';
import { path } from './api-request';

interface Aborts {
  signal?: AbortSignal;
}

export const adminApi = {
  getSeriesOCR: ({ signal }: Aborts = {}) => request<{ enabled: boolean }>('/admin/series-ocr', { signal, cache: 'no-store' }),
  setSeriesOCR: (enabled: boolean) => request<{ enabled: boolean }>('/admin/series-ocr', { method: 'PATCH', body: { enabled } }),
  getRegistration: ({ signal }: Aborts = {}) => request<{ enabled: boolean }>('/admin/registration', { signal, cache: 'no-store' }),
  setRegistration: (enabled: boolean) => request<{ enabled: boolean }>('/admin/registration', { method: 'PATCH', body: { enabled } }),
  listUsers: (q: string, page: number, { signal }: Aborts = {}) => request<AdminUsersPage>('/admin/users', { query: { q, page }, signal, cache: 'no-store' }),
  createUser: (input: AdminUserInput) => request<AdminUser>('/admin/users', { method: 'POST', body: input }),
  updateUser: (userId: string, input: Partial<AdminUserInput>) => request<AdminUser>(path`/admin/users/${userId}`, { method: 'PATCH', body: input }),
  deleteUser: (userId: string) => request<void>(path`/admin/users/${userId}`, { method: 'DELETE' }),
};
