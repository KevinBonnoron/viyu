import { audioTrackIndex, audioTrackOptions, originalAudioTrackIndex } from '../src/features/video/utils/audio-tracks';
import { expect, test } from 'bun:test';

test('the single unknown Default track is selected without falsely claiming an original language', () => {
  expect(audioTrackOptions([{ name: 'Default' }], '', -1)).toEqual([{ language: 'track-0', name: 'Default', original: false, selected: true }]);
  expect(audioTrackIndex([{ name: 'Default' }], 'track-0')).toBe(0);
});

test('the selected dub remains selected while the original language is identified separately', () => {
  const tracks = [
    { lang: 'en-US', name: 'English' },
    { lang: 'fr-FR', name: 'French' },
  ];
  const options = audioTrackOptions(tracks, 'fr-FR', 0);
  expect(options.find((track) => track.original)?.language).toBe('fr-FR');
  expect(options.filter((track) => track.selected).map((track) => track.language)).toEqual(['en-US']);
});

test('provider original-track metadata works without a cached original-language field', () => {
  const tracks = [
    { lang: 'en-US', name: 'American English - dubbed-auto' },
    { lang: 'fr-FR', name: 'Français (France) - original' },
  ];
  expect(originalAudioTrackIndex(tracks, '')).toBe(1);
  expect(
    audioTrackOptions(tracks, '', 1)
      .filter((track) => track.selected && track.original)
      .map((track) => track.language),
  ).toEqual(['fr-FR']);
});
