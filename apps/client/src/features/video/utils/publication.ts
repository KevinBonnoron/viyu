const DAY_LENGTH = 10;
const MIDNIGHT = /^[ T]00:00:00(\.0+)?Z?$/;

/**
 * Turns the PocketBase `published` date ("2026-11-09 07:02:19.123Z", empty when unknown) into the
 * publication the API gives: "YYYY-MM-DD" when only the day is known (stored at UTC midnight), an
 * RFC3339 UTC timestamp when the exact time is known, "" when unknown.
 */
export function publishedFromRecord(value: string | null | undefined): string {
  if (!value) {
    return '';
  }
  const day = value.slice(0, DAY_LENGTH);
  const time = value.slice(DAY_LENGTH);
  return MIDNIGHT.test(time) ? day : `${day}T${time.replace(/^[ T]/, '')}`;
}

/** Whether a publication carries an exact time, not only a day. */
export function hasPublicationTime(published: string): boolean {
  return published.length > DAY_LENGTH;
}

/** The instant of a publication, or null when it is unknown or unreadable. */
export function publicationInstant(published: string): Date | null {
  if (!published) {
    return null;
  }
  const date = new Date(published);
  return Number.isNaN(date.getTime()) ? null : date;
}

/** The publication a card shows: its own, made exact by the fetched one when that is the same day with a time; the fetched one when it has none. */
export function mergePublication(own: string, fetched: string | undefined): string {
  if (!own) {
    return fetched ?? '';
  }
  return !hasPublicationTime(own) && fetched && hasPublicationTime(fetched) && fetched.slice(0, DAY_LENGTH) === own ? fetched : own;
}

/** Orders publications newest first, unknown ones last. */
export function comparePublicationsNewestFirst(a: string, b: string): number {
  const first = publicationInstant(a)?.getTime();
  const second = publicationInstant(b)?.getTime();
  if (first === undefined || second === undefined) {
    return Number(first === undefined) - Number(second === undefined);
  }
  return second - first;
}
