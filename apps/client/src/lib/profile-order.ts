/** The positions to write so that the profiles read in `orderedIds` order: only those whose position differs. */
export function profileOrderChanges(profiles: readonly { id: string; position?: number }[], orderedIds: readonly string[]): { id: string; position: number }[] {
  const current = new Map(profiles.map((profile) => [profile.id, profile.position]));
  return orderedIds.flatMap((id, position) => (current.get(id) === position ? [] : [{ id, position }]));
}

/** `ids` with the item at `from` moved to `to`. */
export function moveId(ids: readonly string[], from: number, to: number): string[] {
  const next = [...ids];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}
