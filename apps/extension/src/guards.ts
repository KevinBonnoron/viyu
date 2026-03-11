/**
 * The one object guard of the extension: YouTube's responses and Viyu's pairing payload are arbitrary JSON, read
 * field by field after this check with `typeof`.
 */
export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
