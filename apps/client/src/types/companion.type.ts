/** A pairing code issued for one profile: shown once, valid until `expiresAt`, usable by a single extension. */
export interface CompanionPairing {
  code: string;
  /** RFC 3339 time after which the code is no longer accepted. */
  expiresAt: string;
}

/** A browser extension paired to a profile of the account. The device's token is never exposed. */
export interface CompanionDevice {
  id: string;
  name: string;
  /** The profile the device syncs into. */
  profile: string;
  profileName: string;
  created: string;
  /** RFC 3339 time of the last sync; empty when the device never synced. */
  lastSyncAt: string;
}
