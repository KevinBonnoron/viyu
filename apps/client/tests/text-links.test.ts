import { parseVideoDescription } from '../src/features/video/utils/description';
import { safeHref, splitLinks, trimUrl } from '../src/lib/text-links';
import { expect, test } from 'bun:test';

const joined = (text: string) =>
  splitLinks(text)
    .map((part) => part.text)
    .join('');

test('a web address in a message becomes a link and the rest stays text', () => {
  expect(splitLinks('go to https://sshhole.com/ now')).toEqual([
    { text: 'go to ', offset: 0 },
    { text: 'https://sshhole.com/', offset: 6, href: 'https://sshhole.com/' },
    { text: ' now', offset: 26 },
  ]);
  expect(splitLinks('https://sshhole.com/')).toEqual([{ text: 'https://sshhole.com/', offset: 0, href: 'https://sshhole.com/' }]);
  expect(splitLinks('no link here')).toEqual([{ text: 'no link here', offset: 0 }]);
  expect(splitLinks('')).toEqual([]);
});

test('punctuation that ends a sentence is not part of the link, a bracket the address opened is', () => {
  expect(trimUrl('https://a.example/x.')).toBe('https://a.example/x');
  expect(trimUrl('https://a.example/x?!')).toBe('https://a.example/x');
  expect(trimUrl('https://a.example/x)')).toBe('https://a.example/x');
  expect(trimUrl('https://en.wikipedia.org/wiki/Foo_(bar)')).toBe('https://en.wikipedia.org/wiki/Foo_(bar)');
  expect(splitLinks('(see https://a.example/x).')).toEqual([
    { text: '(see ', offset: 0 },
    { text: 'https://a.example/x', offset: 5, href: 'https://a.example/x' },
    { text: ').', offset: 24 },
  ]);
});

test('several links keep their order and no character is lost', () => {
  const text = 'a https://one.example, b http://two.example/p?q=1&r=2 c https://one.example';
  expect(splitLinks(text).filter((part) => part.href)).toHaveLength(3);
  expect(joined(text)).toBe(text);
  for (const sample of ['https://x', 'x https://a.example/b?c#d y', '(https://a.example)', 'https://a.example/\nhttps://b.example']) {
    expect(joined(sample)).toBe(sample);
  }
});

test('only http and https are ever opened, whatever the text says', () => {
  expect(safeHref('https://a.example/x')).toBe('https://a.example/x');
  expect(safeHref('http://a.example')).toBe('http://a.example/');
  for (const unsafe of ['javascript:alert(1)', 'data:text/html,x', 'ftp://a.example', 'file:///etc/passwd', 'not a url', '']) {
    expect(safeHref(unsafe)).toBeNull();
  }
  // A scheme in the middle of a word is not a link either: the pattern needs http(s):// itself.
  expect(splitLinks('javascript:alert(1) and data:text/html,x').some((part) => part.href)).toBe(false);
});

test('the description parser still finds addresses and timestamps through the same rules', () => {
  const parts = parseVideoDescription('Chapters 0:00 intro, see https://a.example/x). 1:30 end', 600);
  expect(parts.filter((part) => part.kind === 'url').map((part) => part.text)).toEqual(['https://a.example/x']);
  expect(parts.filter((part) => part.kind === 'time').map((part) => part.text)).toEqual(['0:00', '1:30']);
});
