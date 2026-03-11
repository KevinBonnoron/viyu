import {
  appendMessages,
  contrastRatio,
  countUnread,
  emoteUrl,
  parseChatMessage,
  readableNameColor,
  splitMentions,
  withoutReplyMention,
} from '../src/lib/chat-messages';
import type { ChatMessage } from '../src/types/chat.type';
import { expect, test } from 'bun:test';

function message(id: string): ChatMessage {
  return { id, user: 'viewer', fragments: [{ text: id }] };
}

test('appending keeps the last messages and ignores ids already shown', () => {
  const current = [message('a'), message('b')];
  expect(appendMessages(current, [message('b')])).toBe(current);
  expect(appendMessages(current, [message('c'), message('c'), message('d')], 3).map((item) => item.id)).toEqual(['b', 'c', 'd']);
  expect(
    appendMessages(
      [],
      Array.from({ length: 300 }, (_, index) => message(`m${index}`)),
    ),
  ).toHaveLength(200);
});

test('a chat message is parsed from the server event with its emotes and colour', () => {
  const parsed = parseChatMessage(JSON.stringify({ id: 'x', user: 'Viewer', color: '#FF4500', fragments: [{ text: 'hi ' }, { text: 'Kappa', emote: '25' }] }));
  expect(parsed).toEqual({ id: 'x', user: 'Viewer', color: '#FF4500', fragments: [{ text: 'hi ' }, { text: 'Kappa', emote: '25' }] });
  expect(parseChatMessage(JSON.stringify({ id: 'x', at: 12.5, user: 'Viewer', fragments: [] }))).toEqual({ id: 'x', at: 12.5, user: 'Viewer', fragments: [] });
});

test('malformed chat events are dropped and unsafe colours ignored', () => {
  expect(parseChatMessage('not json')).toBeNull();
  expect(parseChatMessage('null')).toBeNull();
  expect(parseChatMessage('{}')).toBeNull();
  expect(parseChatMessage(JSON.stringify({ id: '', user: 'a', fragments: [] }))).toBeNull();
  expect(parseChatMessage(JSON.stringify({ id: 'x', user: 'a' }))).toBeNull();
  expect(parseChatMessage(JSON.stringify({ id: 'x', user: 'a', fragments: [{ emote: '25' }] }))).toBeNull();
  expect(parseChatMessage(JSON.stringify({ id: 'x', user: 'a', color: 'red; background:url(x)', fragments: [] }))?.color).toBeUndefined();
});

test('an emote is an image of the Twitch CDN, whatever the id holds', () => {
  expect(emoteUrl('25')).toBe('https://static-cdn.jtvnw.net/emoticons/v2/25/default/dark/1.0');
  expect(emoteUrl('a/../b')).toBe('https://static-cdn.jtvnw.net/emoticons/v2/a%2F..%2Fb/default/dark/1.0');
});

test('a user colour is used only when it can be read on the theme surface', () => {
  expect(contrastRatio('#000000', 0.99)).toBeGreaterThan(15);
  expect(readableNameColor('#000000', false)).toBe('#000000');
  expect(readableNameColor('#000000', true)).toBeUndefined();
  expect(readableNameColor('#FFFFFF', true)).toBe('#FFFFFF');
  expect(readableNameColor('#FFFFFF', false)).toBeUndefined();
  expect(readableNameColor('#1E90FF', true)).toBe('#1E90FF');
  expect(readableNameColor('#0000ff', true)).toBeUndefined();
  expect(readableNameColor(undefined, true)).toBeUndefined();
  expect(readableNameColor('blue', true)).toBeUndefined();
  expect(readableNameColor('#12345', false)).toBeUndefined();
});

test('unread messages are the ones after the last one that was seen', () => {
  const all = ['a', 'b', 'c', 'd'].map(message);
  expect(countUnread(all, 'b')).toBe(2);
  expect(countUnread(all, 'd')).toBe(0);
  expect(countUnread([], 'a')).toBe(0);
});

test('everything is unread when nothing was seen or the seen message left the buffer', () => {
  const all = ['c', 'd'].map(message);
  expect(countUnread(all, undefined)).toBe(2);
  expect(countUnread(all, 'a')).toBe(2);
});

test('the message a reply answers is parsed, and dropped when it has no author or id', () => {
  const base = { id: 'x', user: 'Sierra', fragments: [{ text: '@Rhiboz hi' }] };
  expect(parseChatMessage(JSON.stringify({ ...base, reply: { id: 'p', user: 'Rhiboz', text: 'same password' } }))?.reply).toEqual({
    id: 'p',
    user: 'Rhiboz',
    text: 'same password',
  });
  expect(parseChatMessage(JSON.stringify({ ...base, reply: { id: 'p', user: 'Rhiboz' } }))?.reply).toEqual({ id: 'p', user: 'Rhiboz', text: '' });
  for (const reply of [{ id: '', user: 'Rhiboz', text: 't' }, { id: 'p', user: '', text: 't' }, { user: 'Rhiboz' }, 'p', null, 3]) {
    expect(parseChatMessage(JSON.stringify({ ...base, reply }))).toEqual(base);
  }
});

test('mentions are split from the text without losing a character', () => {
  expect(splitMentions('@Rhiboz C’est le même')).toEqual([{ text: '@Rhiboz', mention: 'Rhiboz' }, { text: ' C’est le même' }]);
  expect(splitMentions('gg @a_b and @cd!')).toEqual([
    { text: 'gg ' },
    { text: '@a_b', mention: 'a_b' },
    { text: ' and ' },
    { text: '@cd', mention: 'cd' },
    { text: '!' },
  ]);
  expect(splitMentions('no mention')).toEqual([{ text: 'no mention' }]);
  expect(splitMentions('')).toEqual([]);
  // An `@` inside a word, or a lone `@`, is not a mention.
  expect(splitMentions('mail me@host.com or @ x')).toEqual([{ text: 'mail me@host.com or @ x' }]);
  for (const text of ['@Rhiboz hey @Rhiboz', 'a@bc d@ef', '@@xx', '@x']) {
    expect(
      splitMentions(text)
        .map((part) => part.text)
        .join(''),
    ).toBe(text);
  }
});

test('the mention of the answered author is dropped from a reply, and only that one', () => {
  const reply = { id: 'p', user: 'Rhiboz', text: 'x' };
  expect(withoutReplyMention([{ text: '@Rhiboz C’est le même' }], reply)).toEqual([{ text: 'C’est le même' }]);
  expect(withoutReplyMention([{ text: '@rhiboz  yes' }, { text: 'Kappa', emote: '25' }], reply)).toEqual([{ text: 'yes' }, { text: 'Kappa', emote: '25' }]);
  expect(withoutReplyMention([{ text: '@Rhiboz ' }, { text: 'Kappa', emote: '25' }], reply)).toEqual([{ text: 'Kappa', emote: '25' }]);
  // Left alone: another name, a longer name, no reply, an emote first, and a message that is only the mention.
  expect(withoutReplyMention([{ text: '@Other hi' }], reply)).toEqual([{ text: '@Other hi' }]);
  expect(withoutReplyMention([{ text: '@Rhiboz2 hi' }], reply)).toEqual([{ text: '@Rhiboz2 hi' }]);
  expect(withoutReplyMention([{ text: '@Rhiboz hi' }], undefined)).toEqual([{ text: '@Rhiboz hi' }]);
  expect(withoutReplyMention([{ text: 'Kappa', emote: '25' }], reply)).toEqual([{ text: 'Kappa', emote: '25' }]);
  expect(withoutReplyMention([{ text: '@Rhiboz' }], reply)).toEqual([{ text: '@Rhiboz' }]);
});
