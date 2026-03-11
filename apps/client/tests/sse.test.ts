import { parseSseChunks, reconnectDelay, SseParser } from '../src/lib/sse';
import { expect, test } from 'bun:test';

test('an event is dispatched at its blank line, `message` by default', () => {
  expect(parseSseChunks(['data: hello\n\n'])).toEqual([{ event: 'message', data: 'hello', id: '' }]);
});

test('named events, ids and several events in one chunk', () => {
  expect(parseSseChunks(['event: ready\ndata: {}\nid: 7\n\nevent: message\ndata: x\n\n'])).toEqual([
    { event: 'ready', data: '{}', id: '7' },
    { event: 'message', data: 'x', id: '7' },
  ]);
});

test('every line ending is understood: \\n, \\r\\n and \\r', () => {
  for (const eol of ['\n', '\r\n', '\r']) {
    expect(parseSseChunks([`event: a${eol}data: 1${eol}${eol}data: 2${eol}${eol}`])).toEqual([
      { event: 'a', data: '1', id: '' },
      { event: 'message', data: '2', id: '' },
    ]);
  }
});

test('a \\r\\n split between two chunks is one line end, not a blank line', () => {
  expect(parseSseChunks(['data: a\r', '\ndata: b\r\n\r', '\n'])).toEqual([{ event: 'message', data: 'a\nb', id: '' }]);
});

test('multi-line data is joined with newlines', () => {
  expect(parseSseChunks(['data: one\ndata: two\ndata:\ndata: four\n\n'])).toEqual([{ event: 'message', data: 'one\ntwo\n\nfour', id: '' }]);
});

test('comments are ignored, even between the lines of an event', () => {
  expect(parseSseChunks([': keep-alive\n\ndata: a\n: ping\ndata: b\n\n'])).toEqual([{ event: 'message', data: 'a\nb', id: '' }]);
});

test('the space after the colon is optional, and only one is removed', () => {
  expect(parseSseChunks(['data:tight\n\ndata:  two spaces\n\n'])).toEqual([
    { event: 'message', data: 'tight', id: '' },
    { event: 'message', data: ' two spaces', id: '' },
  ]);
});

test('a field with no colon has an empty value; an event without data is not dispatched', () => {
  expect(parseSseChunks(['data\n\n'])).toEqual([{ event: 'message', data: '', id: '' }]);
  expect(parseSseChunks(['event: orphan\n\ndata: kept\n\n'])).toEqual([{ event: 'message', data: 'kept', id: '' }]);
});

test('an event split across chunks is dispatched once complete; the partial one is kept', () => {
  const parser = new SseParser();
  expect(parser.push('event: mess')).toEqual([]);
  expect(parser.push('age\ndata: {"a":')).toEqual([]);
  expect(parser.push('1}\n')).toEqual([]);
  expect(parser.push('\nevent: next\nda')).toEqual([{ event: 'message', data: '{"a":1}', id: '' }]);
  expect(parser.push('ta: 2\n\n')).toEqual([{ event: 'next', data: '2', id: '' }]);
});

test('a stream cut mid-event never dispatches the unfinished event', () => {
  expect(parseSseChunks(['data: done\n\ndata: unfinished\n'])).toEqual([{ event: 'message', data: 'done', id: '' }]);
});

test('the id persists to the following events, and an id with a NUL is ignored', () => {
  expect(parseSseChunks(['id: 1\ndata: a\n\ndata: b\n\nid: x\0y\ndata: c\n\n'])).toEqual([
    { event: 'message', data: 'a', id: '1' },
    { event: 'message', data: 'b', id: '1' },
    { event: 'message', data: 'c', id: '1' },
  ]);
});

test('a byte order mark at the start of the stream is dropped', () => {
  expect(parseSseChunks(['\ufeffdata: a\n\n'])).toEqual([{ event: 'message', data: 'a', id: '' }]);
});

test('byte-by-byte delivery gives the same events', () => {
  const stream = ': hi\r\nevent: ready\r\ndata: {}\r\n\r\ndata: a\rdata: b\r\r';
  const parser = new SseParser();
  const events = [...stream].flatMap((char) => parser.push(char));
  expect(events).toEqual(parseSseChunks([stream]));
  expect(events).toEqual([
    { event: 'ready', data: '{}', id: '' },
    { event: 'message', data: 'a\nb', id: '' },
  ]);
});

test('reconnection delays grow from one second to a 30 second cap, with a quarter of jitter at most', () => {
  expect(reconnectDelay(0, 0)).toBe(1000);
  expect(reconnectDelay(1, 0)).toBe(2000);
  expect(reconnectDelay(4, 0)).toBe(16000);
  expect(reconnectDelay(5, 0)).toBe(30000);
  expect(reconnectDelay(50, 0)).toBe(30000);
  expect(reconnectDelay(0, 0.999)).toBeLessThanOrEqual(1250);
  expect(reconnectDelay(50, 0.999)).toBeLessThanOrEqual(37500);
});
