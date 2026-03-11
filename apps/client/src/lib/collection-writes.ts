/** What PocketBase fills in on a new record; the collections' item type still carries them. */
export const SYSTEM_FIELDS = { collectionId: '', collectionName: '', created: '', updated: '' } as const;

/** Whether a failed write was refused for validation (a unique index hit, an invalid field): PocketBase answers 400. */
export function isValidationFailure(failure: unknown): boolean {
  return !!failure && typeof failure === 'object' && 'status' in failure && failure.status === 400;
}
