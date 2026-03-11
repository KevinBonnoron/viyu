import type { PocketBaseRecord } from './base.type';

export interface ProfileRecord extends PocketBaseRecord {
  user: string;
  name: string;
  /** Key of one of the profile icons; empty means the default icon. */
  icon?: string;
  /** Accent palette name; empty means the device palette. */
  theme?: string;
  /** Place in the account's profile list, chosen by the viewer; ties (profiles never reordered) keep their creation order. */
  position?: number;
}
