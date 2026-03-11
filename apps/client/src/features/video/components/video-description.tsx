import { useMemo } from 'react';
import { ExternalLink } from '@/components/linked-text';
import { parseVideoDescription } from '@/features/video/utils/description';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';

export function VideoDescription({
  description,
  duration,
  collapsed,
  onSeek,
}: {
  description: string;
  duration: number;
  collapsed: boolean;
  onSeek: (seconds: number) => void;
}) {
  const { t } = useI18n();
  const parts = useMemo(() => parseVideoDescription(description, duration), [description, duration]);
  return (
    <p className={cn('whitespace-pre-line text-sm leading-relaxed [overflow-wrap:anywhere]', collapsed && 'line-clamp-3')}>
      {parts.map((part) =>
        part.kind === 'time' ? (
          <button
            key={part.offset}
            type="button"
            className="rounded-sm text-primary underline decoration-primary/40 underline-offset-2 hover:decoration-primary"
            aria-label={t('player.description.seek', { time: part.text })}
            onClick={() => onSeek(part.seconds)}
          >
            {part.text}
          </button>
        ) : part.kind === 'url' ? (
          <ExternalLink key={part.offset} href={part.href}>
            {part.text}
          </ExternalLink>
        ) : (
          <span key={part.offset}>{part.text}</span>
        ),
      )}
    </p>
  );
}
