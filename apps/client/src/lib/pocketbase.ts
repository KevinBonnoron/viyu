import PocketBase from 'pocketbase';
import { joinServerUrl } from '@/lib/server-url';

// The server base the app talks to: the page's own origin on the web, the configured server (path prefix included) in a native shell.
let apiBase = window.location.origin;

export const pb = new PocketBase(apiBase);

export function setApiBase(base: string): void {
  apiBase = base.replace(/\/+$/, '');
  pb.baseURL = apiBase;
}

/** The server base as configured, without a trailing slash; it may carry a path prefix (`https://host/viyu`). */
export function serverBase(): string {
  return apiBase;
}

/** An absolute URL of the server for a path starting with `/`; the base's own path prefix is kept. */
export function apiUrl(path: string): string {
  return joinServerUrl(apiBase, path);
}
