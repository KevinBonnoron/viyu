import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0) {
    return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  }
  return `${m}:${String(s).padStart(2, '0')}`;
}

const compactNumberFormats = new Map<string, Intl.NumberFormat>();

export function formatCompactNumber(value: number, language: string): string {
  let formatter = compactNumberFormats.get(language);
  if (!formatter) {
    formatter = new Intl.NumberFormat(language, { notation: 'compact', maximumFractionDigits: 1 });
    compactNumberFormats.set(language, formatter);
  }
  return formatter.format(value);
}

const relativeTimeFormats = new Map<string, Intl.RelativeTimeFormat>();

export function formatRelativePublication(date: Date, language: string, now = new Date(), hasTime = false): string {
  const day = 86_400_000;
  const days = Math.round(
    (Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()) - Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())) / day,
  );
  const magnitude = Math.abs(days);
  const [unit, size]: [Intl.RelativeTimeFormatUnit, number] =
    magnitude < 7 ? ['day', 1] : magnitude < 30 ? ['week', 7] : magnitude < 365 ? ['month', 30] : ['year', 365];
  let formatter = relativeTimeFormats.get(language);
  if (!formatter) {
    formatter = new Intl.RelativeTimeFormat(language, { numeric: 'auto' });
    relativeTimeFormats.set(language, formatter);
  }
  if (hasTime && days === 0) {
    const minutes = Math.trunc((date.getTime() - now.getTime()) / 60_000);
    return Math.abs(minutes) < 60 ? formatter.format(minutes, 'minute') : formatter.format(Math.trunc(minutes / 60), 'hour');
  }
  return formatter.format(Math.sign(days) * Math.floor(magnitude / size), unit);
}

/** Watch time as people read it: "12 h 05" in French, "12h 05m" in English; under an hour, minutes only. Rounded to the nearest minute. */
export function formatWatchTime(seconds: number, language: string): string {
  const total = Number.isFinite(seconds) && seconds > 0 ? Math.round(seconds / 60) : 0;
  const hours = Math.floor(total / 60);
  const minutes = total % 60;
  const french = language.toLowerCase().startsWith('fr');
  if (hours === 0) {
    return french ? `${minutes} min` : `${minutes}m`;
  }
  const grouped = hours.toLocaleString(language);
  const padded = String(minutes).padStart(2, '0');
  return french ? `${grouped} h ${padded}` : `${grouped}h ${padded}m`;
}

/** A `YYYY-MM` month in the reader's language: "March 2026" (long) or "Mar 26" (short). An unreadable month is returned as is. */
export function formatMonth(month: string, language: string, style: 'long' | 'short' = 'long'): string {
  const match = /^(\d{4})-(\d{2})$/.exec(month);
  if (!match || Number(match[2]) < 1 || Number(match[2]) > 12) {
    return month;
  }
  const date = new Date(Date.UTC(Number(match[1]), Number(match[2]) - 1, 1));
  return date.toLocaleDateString(
    language,
    style === 'long' ? { month: 'long', year: 'numeric', timeZone: 'UTC' } : { month: 'short', year: '2-digit', timeZone: 'UTC' },
  );
}

/** The width of a bar as a percentage of the largest value; 0 when there is nothing to compare to. */
export function barPercent(value: number, max: number): number {
  return Number.isFinite(value) && Number.isFinite(max) && max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;
}
