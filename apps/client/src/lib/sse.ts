/** One dispatched Server-Sent Event. */
export interface SseEvent {
  /** The `event:` field; `message` when the event names none. */
  event: string;
  /** The `data:` lines, joined by `\n`. */
  data: string;
  /** The last `id:` the stream set (it persists across events, as `Last-Event-ID` does). */
  id: string;
}

/**
 * An incremental parser of the `text/event-stream` format. Feed it decoded text chunks in order, cut anywhere (even in the
 * middle of a line or between the `\r` and `\n` of a line end); it returns the events a chunk completed. What a chunk left
 * unfinished is kept until a later chunk completes it. Lines end with `\n`, `\r\n` or `\r`; `:` lines are comments (the
 * keep-alives of a server); a field's one space after the colon is optional; an event with no `data:` is not dispatched.
 */
export class SseParser {
  private buffer = '';
  private data: string[] = [];
  private event = '';
  private id = '';
  private started = false;
  /** The previous chunk ended in `\r`: a `\n` opening this one belongs to the same line end. */
  private skipLineFeed = false;

  public push(chunk: string): SseEvent[] {
    let text = chunk;
    if (!this.started && text.length > 0) {
      this.started = true;
      if (text.charCodeAt(0) === 0xfeff) {
        text = text.slice(1);
      }
    }
    if (this.skipLineFeed && text.length > 0) {
      this.skipLineFeed = false;
      if (text.charCodeAt(0) === 10) {
        text = text.slice(1);
      }
    }
    text = this.buffer + text;
    const events: SseEvent[] = [];
    let start = 0;
    for (let index = 0; index < text.length; index++) {
      const code = text.charCodeAt(index);
      if (code !== 10 && code !== 13) {
        continue;
      }
      this.line(text.slice(start, index), events);
      if (code === 13) {
        if (text.charCodeAt(index + 1) === 10) {
          index++;
        } else if (index + 1 === text.length) {
          this.skipLineFeed = true;
        }
      }
      start = index + 1;
    }
    this.buffer = text.slice(start);
    return events;
  }

  private line(line: string, events: SseEvent[]): void {
    if (line === '') {
      if (this.data.length > 0) {
        events.push({ event: this.event || 'message', data: this.data.join('\n'), id: this.id });
      }
      this.data = [];
      this.event = '';
      return;
    }
    if (line.charCodeAt(0) === 58) {
      return;
    }
    const colon = line.indexOf(':');
    const field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? '' : line.slice(colon + 1);
    if (value.charCodeAt(0) === 32) {
      value = value.slice(1);
    }
    if (field === 'data') {
      this.data.push(value);
    } else if (field === 'event') {
      this.event = value;
    } else if (field === 'id' && !value.includes('\0')) {
      this.id = value;
    }
  }
}

/** Parses a whole stream given as chunks; for a stream read incrementally, keep a `SseParser`. */
export function parseSseChunks(chunks: readonly string[]): SseEvent[] {
  const parser = new SseParser();
  return chunks.flatMap((chunk) => parser.push(chunk));
}

/**
 * Reads a `text/event-stream` response body to its end, calling `onEvent` for each event as it arrives. It resolves when
 * the server ends the stream and rejects when the connection breaks; aborting `signal` cancels the read (a rejection with
 * the abort reason). This is the browser's SSE client without its limits: the request is a plain `fetch`, so it can carry an
 * `Authorization` header.
 */
export async function readSse(body: ReadableStream<Uint8Array>, signal: AbortSignal, onEvent: (event: SseEvent) => void): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder('utf-8', { fatal: false });
  const parser = new SseParser();
  const cancel = () => void reader.cancel().catch(() => undefined);
  signal.addEventListener('abort', cancel, { once: true });
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        return;
      }
      for (const event of parser.push(decoder.decode(value, { stream: true }))) {
        onEvent(event);
      }
    }
  } finally {
    signal.removeEventListener('abort', cancel);
    reader.releaseLock();
  }
}

/** How long to wait before the next reconnection: exponential from one second, capped at 30, with up to a quarter of jitter. `attempt` counts from 0; `random` is in [0, 1). */
export function reconnectDelay(attempt: number, random: number): number {
  const base = Math.min(30_000, 1000 * 2 ** Math.max(0, attempt));
  return Math.round(base * (1 + 0.25 * random));
}

/** Resolves after `ms`, or at once when `signal` aborts. */
export function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const finish = () => {
      window.clearTimeout(timer);
      signal.removeEventListener('abort', finish);
      resolve();
    };
    const timer = window.setTimeout(finish, ms);
    signal.addEventListener('abort', finish, { once: true });
  });
}
