import { ArrowDown, CornerUpLeft } from 'lucide-react';
import { memo, type RefObject, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { LinkedText } from '@/components/linked-text';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useChatLive } from '@/features/video/hooks/use-chat-live';
import { useChatReplay } from '@/features/video/hooks/use-chat-replay';
import { countUnread, emoteUrl, readableNameColor, splitMentions, withoutReplyMention } from '@/lib/chat-messages';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import { useTheme } from '@/providers/theme-provider';
import type { ChatMessage } from '@/types';

/** How close to the end the list counts as "following" the newest message, in pixels. */
const FOLLOW_THRESHOLD = 32;
const SKELETON_ROWS = [
  { id: 'first', name: 'w-20', text: 'w-40' },
  { id: 'second', name: 'w-16', text: 'w-52' },
  { id: 'third', name: 'w-24', text: 'w-28' },
  { id: 'fourth', name: 'w-14', text: 'w-44' },
  { id: 'fifth', name: 'w-20', text: 'w-36' },
  { id: 'sixth', name: 'w-28', text: 'w-48' },
  { id: 'seventh', name: 'w-16', text: 'w-32' },
  { id: 'eighth', name: 'w-24', text: 'w-40' },
];

type ColorOf = (user: string) => string | undefined;

/** An `@name`: tinted with the colour of the person it names when that person spoke in this chat, with the accent otherwise. */
function Mention({ name, colorOf }: { name: string; colorOf: ColorOf }) {
  const color = colorOf(name);
  return (
    <span
      className={cn('rounded px-1 font-medium', !color && 'bg-primary/10 text-primary')}
      style={color ? { color, backgroundColor: `${color}26` } : undefined}
    >
      @{name}
    </span>
  );
}

/** The message a reply answers, above it. It takes the answered author's colour, and a click brings the message back into view when it is still in the list. */
function ReplyQuote({
  reply,
  colorOf,
  canJump,
  onJump,
}: {
  reply: NonNullable<ChatMessage['reply']>;
  colorOf: ColorOf;
  canJump: boolean;
  onJump: (id: string) => void;
}) {
  const { t } = useI18n();
  const color = colorOf(reply.user);
  const content = (
    <>
      <CornerUpLeft aria-hidden="true" className="size-3 shrink-0" />
      <span className="shrink-0 font-medium" style={color ? { color } : undefined}>
        {t('player.chat.replyingTo')} {reply.user}
      </span>
      {reply.text && <span className="truncate">{reply.text}</span>}
    </>
  );
  const className = 'mb-0.5 flex w-full min-w-0 items-center gap-1.5 border-l-2 pl-2 text-left text-xs text-muted-foreground';
  return canJump ? (
    <button
      type="button"
      title={reply.text}
      onClick={() => onJump(reply.id)}
      className={cn(className, 'rounded-r transition-colors hover:bg-muted/60 hover:text-foreground')}
      style={color ? { borderColor: color } : undefined}
    >
      {content}
    </button>
  ) : (
    <div title={reply.text} className={className} style={color ? { borderColor: color } : undefined}>
      {content}
    </div>
  );
}

const ChatRow = memo(function ChatRow({
  message,
  dark,
  colorOf,
  canJump,
  highlighted,
  onJump,
}: {
  message: ChatMessage;
  dark: boolean;
  colorOf: ColorOf;
  canJump: boolean;
  highlighted: boolean;
  onJump: (id: string) => void;
}) {
  const color = readableNameColor(message.color, dark);
  return (
    <li data-chat-id={message.id} className={cn('break-words px-3 py-1 text-sm leading-snug transition-colors duration-700', highlighted && 'bg-primary/15')}>
      {message.reply && <ReplyQuote reply={message.reply} colorOf={colorOf} canJump={canJump} onJump={onJump} />}
      <span className="font-semibold" style={color ? { color } : undefined}>
        {message.user}
      </span>
      <span className="text-muted-foreground">: </span>
      {withoutReplyMention(message.fragments, message.reply).map((fragment, index) =>
        fragment.emote ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: fragments have no identity beyond their position in the message
          <img key={index} src={emoteUrl(fragment.emote)} alt={fragment.text} loading="lazy" className="mx-0.5 -my-1 inline-block h-6 w-auto align-middle" />
        ) : (
          // biome-ignore lint/suspicious/noArrayIndexKey: fragments have no identity beyond their position in the message
          <span key={index}>
            <LinkedText
              text={fragment.text}
              renderText={(plain) =>
                splitMentions(plain).map((part, partIndex) =>
                  part.mention ? <Mention key={`${part.text}-${partIndex.toString()}`} name={part.mention} colorOf={colorOf} /> : part.text,
                )
              }
            />
          </span>
        ),
      )}
    </li>
  );
});

/** The scrolling list of a chat: the card's only scrollbar. It follows the newest message unless the user scrolled up. */
function ChatMessages({ messages, loading, notice, open }: { messages: ChatMessage[]; loading: boolean; notice?: string; open: boolean }) {
  const { t } = useI18n();
  const { theme } = useTheme();
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  const list = useRef<HTMLDivElement>(null);
  const following = useRef(true);
  const [unseen, setUnseen] = useState(false);
  const newest = messages.at(-1)?.id;
  // Names to the colour their owner chose, as far as this chat has seen them: a row reads it when it is rendered.
  const colors = useRef(new Map<string, string>());
  for (const message of messages) {
    if (message.color) {
      colors.current.set(message.user.toLowerCase(), message.color);
    }
  }
  const colorOf = useCallback<ColorOf>((user) => readableNameColor(colors.current.get(user.toLowerCase()), dark), [dark]);
  const known = useMemo(() => new Set(messages.map((message) => message.id)), [messages]);
  const [highlighted, setHighlighted] = useState<string>();
  useEffect(() => {
    if (!highlighted) {
      return;
    }
    const timer = window.setTimeout(() => setHighlighted(undefined), 1600);
    return () => window.clearTimeout(timer);
  }, [highlighted]);
  const jumpTo = useCallback((id: string) => {
    const element = list.current;
    const target = element ? Array.from(element.querySelectorAll<HTMLElement>('[data-chat-id]')).find((row) => row.dataset.chatId === id) : undefined;
    if (!element || !target) {
      return;
    }
    // Scroll the list itself, never the page around it.
    const offset = target.getBoundingClientRect().top - element.getBoundingClientRect().top;
    element.scrollTo({ top: element.scrollTop + offset - (element.clientHeight - target.clientHeight) / 2, behavior: 'smooth' });
    setHighlighted(id);
  }, []);

  useLayoutEffect(() => {
    const element = list.current;
    if (!element || !newest) {
      return;
    }
    if (following.current) {
      element.scrollTop = element.scrollHeight;
    } else {
      setUnseen(true);
    }
  }, [newest]);

  // The list is hidden while another section is open: when it comes back it goes to the newest message again if it was following.
  useLayoutEffect(() => {
    const element = list.current;
    if (open && element && following.current) {
      element.scrollTop = element.scrollHeight;
    }
  }, [open]);

  // Emotes load after their message is added and make it taller: keep following the newest message while the content grows.
  useEffect(() => {
    const element = list.current;
    const content = element?.firstElementChild;
    if (!element || !content) {
      return;
    }
    const observer = new ResizeObserver(() => {
      if (following.current) {
        element.scrollTop = element.scrollHeight;
      }
    });
    observer.observe(content);
    return () => observer.disconnect();
  }, []);

  const jumpToNewest = () => {
    const element = list.current;
    if (element) {
      element.scrollTop = element.scrollHeight;
    }
    following.current = true;
    setUnseen(false);
  };

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={list}
        role="log"
        aria-live="off"
        aria-label={t('player.chat.title')}
        onScroll={(event) => {
          const element = event.currentTarget;
          following.current = element.scrollHeight - element.scrollTop - element.clientHeight < FOLLOW_THRESHOLD;
          if (following.current) {
            setUnseen(false);
          }
        }}
        className="min-h-0 flex-1 overflow-y-auto py-1"
      >
        <div>
          {messages.length > 0 && (
            <ul>
              {messages.map((message) => (
                <ChatRow
                  key={message.id}
                  message={message}
                  dark={dark}
                  colorOf={colorOf}
                  canJump={!!message.reply && known.has(message.reply.id)}
                  highlighted={highlighted === message.id}
                  onJump={jumpTo}
                />
              ))}
            </ul>
          )}
          {loading && messages.length === 0 && (
            <div role="status" aria-busy="true" aria-label={t('player.chat.loading')}>
              {SKELETON_ROWS.map((row) => (
                <div key={row.id} aria-hidden="true" className="flex items-center gap-2 px-3 py-1.5">
                  <Skeleton className={`h-4 ${row.name}`} />
                  <Skeleton className={`h-4 ${row.text}`} />
                </div>
              ))}
            </div>
          )}
          {notice && !loading && messages.length === 0 && <p className="px-4 py-3 text-sm text-muted-foreground">{notice}</p>}
        </div>
      </div>
      {unseen && (
        <Button type="button" size="sm" variant="secondary" onClick={jumpToNewest} className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-md">
          <ArrowDown aria-hidden="true" />
          {t('player.chat.newMessages')}
        </Button>
      )}
    </div>
  );
}

/** What arrives while the chat is not shown, for the count on its tab; looking at the chat clears it. */
function useUnread(messages: ChatMessage[], open: boolean, report: (count: number) => void) {
  const seen = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (open) {
      seen.current = messages.at(-1)?.id;
      report(0);
    } else {
      report(countUnread(messages, seen.current));
    }
  }, [messages, open, report]);
}

function LiveChat({ login, open, report }: { login: string; open: boolean; report: (count: number) => void }) {
  const { t } = useI18n();
  const { messages, status } = useChatLive(login, true);
  useUnread(messages, open, report);
  return (
    <ChatMessages
      open={open}
      messages={messages}
      loading={status === 'connecting'}
      notice={status === 'unavailable' ? t('player.chat.unavailable') : t('player.chat.empty')}
    />
  );
}

function ReplayChat({
  vodId,
  videoRef,
  version,
  open,
  report,
}: {
  vodId: string;
  videoRef: RefObject<HTMLVideoElement | null>;
  version: number;
  open: boolean;
  report: (count: number) => void;
}) {
  const { t } = useI18n();
  const { messages, loading, error, retry } = useChatReplay(vodId, videoRef, version, true);
  useUnread(messages, open, report);
  return (
    <>
      <ChatMessages open={open} messages={messages} loading={loading} notice={error ? undefined : t('player.chat.empty')} />
      {error && <ErrorSnackbar message={`${t('player.chat.error')} ${t(error)}`} action={t('common.retry')} onAction={retry} />}
    </>
  );
}

/**
 * The chat of a Twitch video: the live chat of a stream in progress (`login` is the channel's login, which Twitch's chat is
 * addressed by), or the replay of a VOD (`videoId` is its record id) synchronised with the playback time of `videoRef`. It is
 * mounted the first time its section is opened and then keeps running and keeps its messages while hidden, reporting how many
 * it received meanwhile through `onUnread`.
 */
export function VideoChat({
  videoId,
  login,
  live,
  videoRef,
  version,
  open,
  onUnread,
}: {
  videoId: string;
  login: string;
  live: boolean;
  videoRef: RefObject<HTMLVideoElement | null>;
  version: number;
  open: boolean;
  onUnread: (count: number) => void;
}) {
  return live ? (
    <LiveChat login={login} open={open} report={onUnread} />
  ) : (
    <ReplayChat vodId={videoId} videoRef={videoRef} version={version} open={open} report={onUnread} />
  );
}
