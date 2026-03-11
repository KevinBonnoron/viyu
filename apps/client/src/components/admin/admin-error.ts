import { ApiError } from '@/lib/api-error';
import type { TranslationKey } from '@/providers/i18n-provider';

export function adminErrorKey(error: unknown): TranslationKey {
  if (!(error instanceof ApiError)) {
    return 'admin.connectionError';
  }
  switch (error.code) {
    case 'ADMIN_SELF_PROTECTION':
      return 'admin.selfProtection';
    case 'ADMIN_LAST_ADMIN':
      return 'admin.lastAdmin';
  }
  if (error.status === 401) {
    return 'admin.sessionError';
  }
  if (error.status === 404) {
    return 'admin.notFound';
  }
  return error.status === 400 ? 'admin.validationError' : 'admin.requestError';
}
