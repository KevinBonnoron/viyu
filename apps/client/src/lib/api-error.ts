import type { TranslationKey } from '@/providers/i18n-provider';

/** A failed API call: `code` is the server's machine-readable error, `message` its description when it gave one. */
export class ApiError extends Error {
  public constructor(
    public readonly code: string,
    public readonly status: number,
    message?: string,
    /** Server-requested pause, in milliseconds (Retry-After); zero when none was provided. */
    public readonly retryAfterMs = 0,
  ) {
    super(message ?? code);
  }
}

/** The error of a failed response from its JSON body: custom routes answer `{code, message}`; anything else keeps the status. */
export function apiErrorFrom(status: number, body: unknown, fallbackCode = 'REQUEST_FAILED', retryAfter: string | null = null): ApiError {
  const field = (name: string): string | undefined => {
    const value = body && typeof body === 'object' ? (body as Record<string, unknown>)[name] : undefined;
    return typeof value === 'string' && value ? value : undefined;
  };
  const seconds = retryAfter?.trim() ? Number(retryAfter) : 0;
  const delay = Number.isFinite(seconds) ? seconds * 1000 : Date.parse(retryAfter ?? '') - Date.now();
  return new ApiError(field('code') ?? fallbackCode, status, field('message'), Number.isFinite(delay) ? Math.max(0, delay) : 0);
}

const sourceErrorKeys: Partial<Record<string, TranslationKey>> = {
  SOURCE_NOT_CONNECTED: 'errors.sourceNotConnected',
  SOURCE_SESSION_INVALID: 'errors.sourceSessionInvalid',
  SOURCE_REQUEST_FAILED: 'errors.sourceRequestFailed',
  SOURCE_SUBSCRIBER_ONLY: 'errors.sourceSubscriberOnly',
  SOURCE_CHANNEL_OFFLINE: 'errors.sourceChannelOffline',
  INVALID_SOURCE_INPUT: 'errors.sourceInvalidInput',
  SOURCE_DISABLED: 'errors.sourceDisabled',
  SOURCE_RATE_LIMITED: 'errors.sourceRateLimited',
  TOO_MANY_REQUESTS: 'errors.tooManyRequests',
  CHAT_TOO_MANY_CONNECTIONS: 'errors.chatTooManyConnections',
  STREAM_UPSTREAM_UNAVAILABLE: 'errors.sourceRequestFailed',
  SHUTTING_DOWN: 'errors.serverRestarting',
};

/** The translation of the message that tells the viewer why a call failed. */
export function requestError(error: unknown): TranslationKey {
  if (!(error instanceof ApiError)) {
    return 'errors.serverUnreachable';
  }
  return (
    (Object.hasOwn(sourceErrorKeys, error.code) ? sourceErrorKeys[error.code] : undefined) ??
    (error.status === 401 ? 'errors.sessionExpired' : error.status === 429 ? 'errors.tooManyRequests' : 'errors.requestFailed')
  );
}
