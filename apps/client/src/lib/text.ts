/** A text reduced to what a search compares: without case, accents or compatibility forms (`ﬁ` → `fi`). */
export function foldSearch(value: string): string {
  return value.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
}
