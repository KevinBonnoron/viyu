import {
  Baby,
  BookOpen,
  Bot,
  Cat,
  Coffee,
  Crown,
  Dog,
  Film,
  Flame,
  Gamepad2,
  Ghost,
  GraduationCap,
  Heart,
  Leaf,
  type LucideIcon,
  Music,
  Rocket,
  Smile,
  Star,
  UserRound,
  Zap,
} from 'lucide-react';
// Relative on purpose: this module is unit-tested, and `bun test` cannot resolve the `@/` alias now that
// TypeScript 7 no longer has `baseUrl`.
import { ACCENTS } from '../providers/theme-provider';

/** Icons a profile can show in the application bar. Keys are stored on the profile record. */
export const PROFILE_ICONS = {
  'user-round': UserRound,
  smile: Smile,
  ghost: Ghost,
  cat: Cat,
  dog: Dog,
  rocket: Rocket,
  'gamepad-2': Gamepad2,
  music: Music,
  film: Film,
  'book-open': BookOpen,
  'graduation-cap': GraduationCap,
  baby: Baby,
  heart: Heart,
  star: Star,
  flame: Flame,
  leaf: Leaf,
  zap: Zap,
  coffee: Coffee,
  crown: Crown,
  bot: Bot,
} as const satisfies Record<string, LucideIcon>;

export type ProfileIconKey = keyof typeof PROFILE_ICONS;
export const PROFILE_ICON_KEYS = Object.keys(PROFILE_ICONS) as ProfileIconKey[];
export const DEFAULT_PROFILE_ICON: ProfileIconKey = 'user-round';

export function isProfileIcon(value: unknown): value is ProfileIconKey {
  return typeof value === 'string' && Object.hasOwn(PROFILE_ICONS, value);
}

/** The icon component for a stored key; an unknown or empty key falls back to the default icon. */
export function profileIcon(value?: string): LucideIcon {
  return isProfileIcon(value) ? PROFILE_ICONS[value] : PROFILE_ICONS[DEFAULT_PROFILE_ICON];
}

export type ProfileTheme = (typeof ACCENTS)[number];

/** The accent palette for a stored value, or undefined to keep the device palette. */
export function profileTheme(value?: string): ProfileTheme | undefined {
  return ACCENTS.find((accent) => accent === value);
}
