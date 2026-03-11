import '@tanstack/history';

declare module '@tanstack/history' {
  interface HistoryState {
    viyuSource?: string;
    viyuSearchPlatform?: string;
  }
}
