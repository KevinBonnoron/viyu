import { ChevronDown, ThumbsUp } from 'lucide-react';
import { useState } from 'react';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { LinkedText } from '@/components/linked-text';
import { Skeleton } from '@/components/ui/skeleton';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { cn, formatCompactNumber } from '@/lib/utils';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { VideoComment } from '@/types';

/** One comment body — author, date, text, likes — shared by top-level comments and their replies. */
function CommentBody({ comment, language }: { comment: VideoComment; language: string }) {
  const { t } = useI18n();
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
      <p className="whitespace-pre-wrap break-words">
        <LinkedText text={comment.text} />
      </p>
      {/* An aria-label instead of a sr-only span: an absolutely positioned descendant's containing
          block is the panel's relative section, so a sr-only here escapes the card's overflow
          clipping and lengthens the page by its static position on every comment. */}
      <span
        role="img"
        aria-label={t('player.comments.likes', { count: comment.likeCount })}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground"
      >
        <ThumbsUp aria-hidden="true" className="size-3" /> <span aria-hidden="true">{formatCompactNumber(comment.likeCount, language)}</span>
      </span>
    </div>
  );
}

/** A top-level comment; its fetched answers sit behind a toggle, flat and one level deep. */
function CommentThread({ comment, language }: { comment: VideoComment; language: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const replies = comment.replies ?? [];
  return (
    <li className="space-y-1 py-4 text-sm first:pt-0 last:pb-0">
      <CommentBody comment={comment} language={language} />
      {replies.length > 0 && (
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className="mt-1 inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium text-primary transition-colors hover:bg-primary/10"
        >
          <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', open && 'rotate-180')} />
          {t(open ? 'player.comments.hideReplies' : 'player.comments.replies', { count: replies.length })}
        </button>
      )}
      {open && (
        <ul className="ml-3 mt-3 space-y-3 border-l-2 border-border/60 pl-3">
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
