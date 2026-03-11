/** Formats a position in seconds the way shared video links carry it: compact, and readable in the URL (`1h2m3s`). */
export function formatTimecode(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const rest = total % 60;
  if (hours > 0) {
    return `${hours}h${minutes}m${rest}s`;
  }
  if (minutes > 0) {
    return `${minutes}m${rest}s`;
  }
  return `${rest}s`;
}

/** Reads a link's start time: plain seconds or the `1h2m3s` form; anything else means "from the start". */
export function parseTimecode(value: string | number | undefined): number | undefined {
  if (typeof value === 'number') {
    return Number.isFinite(value) && value > 0 ? Math.floor(value) : undefined;
  }
  if (typeof value !== 'string' || value.trim() === '') {
    return undefined;
  }
  const text = value.trim();
  if (/^\d+$/.test(text)) {
    return parseTimecode(Number(text));
  }
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/i.exec(text);
  if (!match || match.slice(1).every((part) => part === undefined)) {
    return undefined;
  }
  const hours = Number(match[1] ?? 0);
  const minutes = Number(match[2] ?? 0);
  const seconds = Number(match[3] ?? 0);
  const total = hours * 3600 + minutes * 60 + seconds;
  return total > 0 ? total : undefined;
}
