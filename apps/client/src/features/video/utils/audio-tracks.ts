export interface AudioOption {
  language: string;
  name: string;
  original: boolean;
  selected: boolean;
}

type AudioTrack = { lang?: string; name: string };

export function audioTrackIndex(tracks: readonly AudioTrack[], language: string): number {
  if (!language) {
    return -1;
  }
  if (/^track-\d+$/u.test(language)) {
    const index = Number(language.slice(6));
    return index < tracks.length ? index : -1;
  }
  const desired = language.toLowerCase();
  const exact = tracks.findIndex((track) => track.lang?.toLowerCase() === desired);
  return exact >= 0 ? exact : tracks.findIndex((track) => track.lang?.toLowerCase().split('-')[0] === desired.split('-')[0]);
}

export function originalAudioTrackIndex(tracks: readonly AudioTrack[], language: string): number {
  const identified = audioTrackIndex(tracks, language);
  return identified >= 0 ? identified : tracks.findIndex((track) => /(?:^| - )original$/iu.test(track.name));
}

export function audioTrackOptions(tracks: readonly AudioTrack[], originalLanguage: string, activeIndex: number): AudioOption[] {
  const original = originalAudioTrackIndex(tracks, originalLanguage);
  const selected = activeIndex >= 0 ? activeIndex : tracks.length === 1 ? 0 : -1;
  const options = new Map<string, AudioOption>();
  tracks.forEach((track, index) => {
    const language = track.lang || `track-${index}`;
    const existing = options.get(language);
    if (existing) {
      existing.selected ||= index === selected;
      existing.original ||= index === original;
    } else {
      options.set(language, { language, name: track.name, original: index === original, selected: index === selected });
    }
  });
  return [...options.values()].sort((a, b) => Number(b.original) - Number(a.original));
}
