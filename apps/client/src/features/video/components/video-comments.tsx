import { ChevronDown, ThumbsUp } from 'lucide-react';
import { type ReactNode, useId, useLayoutEffect, useRef, useState } from 'react';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { LinkedText } from '@/components/linked-text';
import { Skeleton } from '@/components/ui/skeleton';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { cn, formatCompactNumber } from '@/lib/utils';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { VideoComment } from '@/types';

/** One comment body and its metadata row, with an optional reply toggle for top-level comments. */
function CommentBody({ comment, language, children }: { comment: VideoComment; language: string; children?: ReactNode }) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const textRef = useRef<HTMLParagraphElement>(null);
  const textId = useId();

  useLayoutEffect(() => {
    const element = textRef.current;
    if (!element) {
      return;
    }
    const measure = () => {
      const lineHeight = Number.parseFloat(getComputedStyle(element).lineHeight);
      setOverflows(element.scrollHeight > lineHeight * 4 + 1);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [comment.text, language]);

  const published = comment.publishedAt ? new Date(comment.publishedAt) : null;
  const date =
    published && !Number.isNaN(published.getTime())
      ? published.toLocaleDateString(language, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' })
      : null;
  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-semibold">{comment.author}</span>
        {date && (
          <time dateTime={comment.publishedAt} className="text-xs text-muted-foreground">
            {date}
          </time>
        )}
      </div>
      <p ref={textRef} id={textId} className={cn('whitespace-pre-wrap break-words leading-5', !expanded && 'line-clamp-4')}>
        <LinkedText text={comment.text} />
      </p>
      {(overflows || expanded) && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={textId}
          onClick={() => setExpanded((value) => !value)}
          className="block rounded-sm text-xs font-medium text-primary hover:underline"
        >
          {t(expanded ? 'player.comments.showLess' : 'player.comments.showMore')}
        </button>
      )}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <span
          role="img"
          aria-label={t('player.comments.likes', { count: comment.likeCount })}
          className="inline-flex shrink-0 items-center gap-1.5 tabular-nums text-muted-foreground"
        >
          <ThumbsUp aria-hidden="true" className="size-3.5" />
          <span aria-hidden="true">{formatCompactNumber(comment.likeCount, language)}</span>
        </span>
        {children}
      </div>
    </div>
  );
}

/** A top-level comment; its fetched answers sit behind a toggle, flat and one level deep. */
function CommentThread({ comment, language }: { comment: VideoComment; language: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const repliesId = useId();
  const replies = comment.replies ?? [];
  return (
    <li className="space-y-1 py-3 text-sm first:pt-0 last:pb-0">
      <CommentBody comment={comment} language={language}>
        {replies.length > 0 && (
          <button
            type="button"
            aria-label={open ? t('player.comments.hideReplies') : undefined}
            aria-expanded={open}
            aria-controls={repliesId}
            onClick={() => setOpen((value) => !value)}
            className="inline-flex min-h-8 items-center gap-1 rounded-md px-1.5 font-medium text-primary transition-colors hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t(replies.length === 1 ? 'player.comments.replies.one' : 'player.comments.replies.other', { count: replies.length })}
            <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', open && 'rotate-180')} />
          </button>
        )}
      </CommentBody>
      {open && (
        <ul
          id={repliesId}
          className="relative mt-3 space-y-3 rounded-xl bg-muted/40 p-3 before:pointer-events-none before:absolute before:-top-3 before:left-3 before:h-3 before:w-3 before:rounded-bl-lg before:border-b before:border-l before:border-border sm:ml-4"
        >
          {replies.map((reply) => (
            <li key={reply.id}>
              <CommentBody comment={reply} language={language} />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

export function VideoComments({ videoId, source }: { videoId: string; source?: string }) {
  const { t, language } = useI18n();
  const [comments, setComments] = useState<VideoComment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<TranslationKey>();
  const [retry, setRetry] = useState(0);

  useAbortableEffect(
    (signal) => {
      setLoading(true);
      setError(undefined);
      return api.videos
        .comments(videoId, { source, signal })
        .then((page) => {
          if (!signal.aborted) {
            setComments(page.comments);
            setLoading(false);
          }
        })
        .catch((cause: unknown) => {
          if (!signal.aborted) {
            setError(requestError(cause));
            setLoading(false);
          }
        });
    },
    [videoId, source, retry],
  );

  return (
    <div className="space-y-4 p-4">
      {comments.length > 0 && (
        <ul className="divide-y">
          {comments.map((comment) => (
            <CommentThread key={comment.id} comment={comment} language={language} />
          ))}
        </ul>
      )}
      {loading && (
        <div role="status" aria-busy="true" aria-label={t('player.comments.loading')} className="divide-y">
          {['first', 'second', 'third', 'fourth', 'fifth', 'sixth', 'seventh', 'eighth', 'ninth', 'tenth'].map((position) => (
            <div key={position} aria-hidden="true" className="space-y-2 py-4 first:pt-0 last:pb-0">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-11/12" />
              <Skeleton className="h-3 w-8" />
            </div>
          ))}
        </div>
      )}
      {error && (
        <ErrorSnackbar message={`${t('player.comments.error')} ${t(error)}`} action={t('common.retry')} onAction={() => setRetry((value) => value + 1)} />
      )}
      {!loading && !error && comments.length === 0 && <p className="text-sm text-muted-foreground">{t('player.comments.empty')}</p>}
    </div>
  );
}
