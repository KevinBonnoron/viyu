import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { filterIdentity, isChannelGroup } from '@/lib/channel-groups';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import type { ChannelFilterItem } from '@/types';

export function ChannelFilters({
  channels,
  selected,
  live,
  onSelect,
}: {
  channels: ChannelFilterItem[];
  selected?: string;
  live?: ReadonlySet<string>;
  onSelect: (key?: string) => void;
}) {
  const { t } = useI18n();
  const viewport = useRef<HTMLDivElement>(null);
  const wrapper = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);
  const [canBack, setCanBack] = useState(false);
  const [canForward, setCanForward] = useState(false);

  useEffect(() => {
    const element = viewport.current;
    if (!element) {
      return;
    }
    const update = () => {
      const maximum = Math.max(0, element.scrollWidth - element.clientWidth);
      setOverflow(maximum > 1);
      setCanBack(element.scrollLeft > 1);
      setCanForward(element.scrollLeft < maximum - 1);
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    if (wrapper.current) {
      observer.observe(wrapper.current);
    }
    if (element.firstElementChild) {
      observer.observe(element.firstElementChild);
    }
    element.addEventListener('scroll', update, { passive: true });
    return () => {
      observer.disconnect();
      element.removeEventListener('scroll', update);
    };
  }, [channels]);

  const scroll = (direction: -1 | 1) => {
    viewport.current?.scrollBy({ left: direction * Math.max(160, (viewport.current?.clientWidth ?? 0) * 0.8), behavior: 'smooth' });
  };
  return (
    <div ref={wrapper} className="flex min-w-0 max-w-full items-center gap-1">
      {overflow && (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t('subscriptions.filter.previous')}
          title={t('subscriptions.filter.previous')}
          disabled={!canBack}
          onClick={() => scroll(-1)}
          className="size-8 shrink-0 rounded-full"
        >
          <ChevronLeft aria-hidden="true" className="size-4" />
        </Button>
      )}
      <div ref={viewport} className="min-w-0 flex-1 overflow-x-auto overscroll-x-contain [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        <div className="flex w-max gap-2 py-1">
          {channels.map((channel) => {
            const key = filterIdentity(channel);
            const avatar = isChannelGroup(channel) ? channel.channels[0] : channel;
            const isLive = !!live?.has(key);
            return (
              <button
                key={key}
                type="button"
                aria-pressed={selected === key}
                title={isLive ? `${channel.name} · ${t('channel.live')}` : channel.name}
                onClick={() => onSelect(selected === key ? undefined : key)}
                className="group flex w-16 shrink-0 flex-col items-center gap-1.5 rounded-lg py-1 text-xs transition-colors hover:bg-muted/70"
              >
                <span
                  className={cn(
                    'relative flex size-12 items-center justify-center rounded-full border-2 transition-[border-color,transform] motion-safe:group-hover:scale-105',
                    selected === key ? 'border-primary' : isLive ? 'border-red-500' : 'border-transparent group-hover:border-border',
                  )}
                >
                  <ChannelAvatar src={avatar.thumbnail} className="size-full" />
                  {isLive && (
                    <span className="absolute -right-0.5 -top-0.5 size-3.5 rounded-full bg-background ring-2 ring-background">
                      <span aria-hidden="true" className="absolute inset-0 rounded-full bg-red-600 motion-safe:animate-pulse" />
                      <span className="sr-only">{t('channel.live')}</span>
                    </span>
                  )}
                  {isChannelGroup(channel) && (
                    <span className="absolute -bottom-1 -right-1.5 flex -space-x-1">
                      {channel.channels.map((member) => (
                        <PlatformBadge key={member.platform} platform={member.platform} className="size-5 rounded-full [&_svg]:size-3" />
                      ))}
                    </span>
                  )}
                </span>
                <span className="block w-full truncate text-center text-muted-foreground transition-colors group-hover:text-foreground">{channel.name}</span>
              </button>
            );
          })}
        </div>
      </div>
      {overflow && (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t('subscriptions.filter.next')}
          title={t('subscriptions.filter.next')}
          disabled={!canForward}
          onClick={() => scroll(1)}
          className="size-8 shrink-0 rounded-full"
        >
          <ChevronRight aria-hidden="true" className="size-4" />
        </Button>
      )}
    </div>
  );
}
