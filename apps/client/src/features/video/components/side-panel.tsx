import { X } from 'lucide-react';
import { type ReactNode, useCallback, useEffect, useId, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useShortcuts } from '@/hooks/useShortcuts';
import { selectSection, sidePanelStorageKey } from '@/lib/side-panel';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';

export interface SidePanelSection {
  id: string;
  label: string;
  icon: ReactNode;
  /** Shown in the panel's header next to the close button. */
  badge?: ReactNode;
  /** Single-letter keyboard shortcut that toggles this section; shown as a colored hint in the tab's tooltip. */
  shortcut?: string;
  /** Whether the panel scrolls this section's content. Content that holds its own scroller sets it to false. */
  scroll?: boolean;
  /**
   * `open` is false while another section is shown: the content stays mounted once it has been opened, so it keeps its
   * state, but should stop doing anything costly. `report` sets the count shown on the section's tab while it is not open
   * (new chat messages, say); report 0 to clear it.
   */
  children: (open: boolean, report: (count: number) => void) => ReactNode;
}

/** Counts above this are shown as "99+", and not tracked any further, so a busy chat does not re-render the panel for every message. */
const INDICATOR_LIMIT = 99;

function storedChoice(context: string): string | null {
  try {
    return localStorage.getItem(sidePanelStorageKey(context));
  } catch {
    return null;
  }
}

/**
 * The player's sections (series, description, comments) as a narrow rail of icon tabs beside the video, one
 * panel open at a time. On a wide-shaped window (see the `beside` variant) the rail and the panel sit to the right of the video and share its height, so the
 * panel never lengthens the page and only its own content scrolls; otherwise they stack under the video. The panel
 * is closed unless the user chose a section (remembered per device and per `context`: a stream, a series episode or a
 * plain video) or the video suggests one (`preferred`).
 */
export function SidePanel({ sections, preferred, context, loading }: { sections: SidePanelSection[]; preferred?: string; context: string; loading?: boolean }) {
  const { t } = useI18n();
  const panelId = useId();
  const [choice, setChoice] = useState<{ context: string; id: string } | null>(null);
  const [mounted, setMounted] = useState<ReadonlySet<string>>(new Set());
  const [indicators, setIndicators] = useState<Record<string, number>>({});
  const reporters = useRef(new Map<string, (count: number) => void>());
  const stored = choice?.context === context ? choice.id : storedChoice(context);
  const selected = selectSection(
    stored,
    sections.map((section) => section.id),
    preferred,
  );
  const current = sections.find((section) => section.id === selected);

  // One stable reporter per section, so the content's effects do not restart each time the panel renders.
  const reporterFor = useCallback((id: string) => {
    let report = reporters.current.get(id);
    if (!report) {
      report = (count: number) => {
        const value = Math.min(Math.max(0, count), INDICATOR_LIMIT + 1);
        setIndicators((previous) => (previous[id] === value ? previous : { ...previous, [id]: value }));
      };
      reporters.current.set(id, report);
    }
    return report;
  }, []);

  useEffect(() => {
    if (selected) {
      setMounted((previous) => (previous.has(selected) ? previous : new Set(previous).add(selected)));
    }
  }, [selected]);

  const choose = (id: string) => {
    setChoice({ context, id });
    try {
      localStorage.setItem(sidePanelStorageKey(context), id);
    } catch {
      // The choice still holds for this visit without storage.
    }
  };

  // A section's letter toggles it, exactly like its tab does; the hint shows in the tab's tooltip.
  useShortcuts(
    Object.fromEntries(
      sections
        .map((section) => [section.shortcut, section] as const)
        .filter((entry): entry is readonly [string, (typeof sections)[number]] => entry[0] !== undefined)
        .map(([shortcut, section]) => [shortcut.toLowerCase(), () => choose(selected === section.id ? '' : section.id)]),
    ),
    !loading && sections.length > 0,
  );

  if (!loading && sections.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-col gap-3 beside:flex-row-reverse beside:self-stretch">
      <nav
        aria-label={t('player.details')}
        className="flex min-w-0 shrink-0 gap-1 beside:flex-col beside:self-start stacked:w-full stacked:rounded-xl stacked:border stacked:bg-muted/40 stacked:p-1"
      >
        {loading
          ? ['first', 'second', 'third'].map((position) => (
              <Skeleton key={position} aria-hidden="true" className="size-10 rounded-lg stacked:h-11 stacked:flex-1" />
            ))
          : sections.map((section) => {
              const count = selected === section.id ? 0 : (indicators[section.id] ?? 0);
              const label =
                count > 0
                  ? t('player.panel.indicator', { label: section.label, count: count > INDICATOR_LIMIT ? `${INDICATOR_LIMIT}+` : count })
                  : section.label;
              return (
                <Button
                  key={section.id}
                  type="button"
                  variant="ghost"
                  size="icon"
                  className={cn(
                    'relative rounded-lg stacked:h-11 stacked:min-w-0 stacked:w-auto stacked:flex-auto stacked:px-2 stacked:text-muted-foreground',
                    selected === section.id &&
                      'bg-primary/10 text-primary hover:bg-primary/15 stacked:bg-background stacked:text-foreground stacked:shadow-sm stacked:hover:bg-background',
                  )}
                  aria-label={label}
                  aria-pressed={selected === section.id}
                  aria-controls={panelId}
                  title={label}
                  data-shortcut={section.shortcut}
                  onClick={() => choose(selected === section.id ? '' : section.id)}
                >
                  <span className="contents stacked:hidden">{section.icon}</span>
                  <span className="hidden min-w-0 truncate text-sm font-medium stacked:block stacked:text-xs">{section.label}</span>
                  {count > 0 && (
                    <span
                      aria-hidden="true"
                      className="absolute -right-1.5 -top-1 min-w-4 rounded-full bg-primary px-1 text-center text-[10px] font-semibold leading-4 text-primary-foreground tabular-nums ring-2 ring-background"
                    >
                      {count > INDICATOR_LIMIT ? `${INDICATOR_LIMIT}+` : count}
                    </span>
                  )}
                </Button>
              );
            })}
      </nav>
      {(current || mounted.size > 0) && (
        <section
          id={panelId}
          aria-label={current?.label}
          hidden={!current}
          className={cn('relative min-w-0 beside:h-auto beside:w-[22rem] min-[1700px]:beside:w-[26rem]', !current && 'hidden')}
        >
          {/* Beside the video the card fills the video's height. Below it, it is as tall as its content up to 32rem (scrolling past that); a section that scrolls by itself (a chat, an episode list) has no content height to follow, so it gets the 32rem. */}
          <div
            className={cn(
              'flex flex-col overflow-hidden rounded-2xl border bg-card beside:absolute beside:inset-0 stacked:max-h-[32rem]',
              current?.scroll === false && 'stacked:h-[32rem]',
            )}
          >
            <header className="flex shrink-0 items-center justify-between gap-3 border-b px-4 py-2.5">
              <h2 className="min-w-0 truncate text-base font-medium">{current?.label}</h2>
              <span className="flex shrink-0 items-center gap-2">
                {current?.badge}
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-8 rounded-full"
                  aria-label={t('player.panel.close')}
                  title={t('player.panel.close')}
                  onClick={() => choose('')}
                >
                  <X aria-hidden="true" className="size-4" />
                </Button>
              </span>
            </header>
            {sections.map((section) =>
              mounted.has(section.id) || section.id === selected ? (
                <div
                  key={section.id}
                  hidden={section.id !== selected}
                  className={cn(
                    section.id === selected ? 'min-h-0 flex-1' : 'hidden',
                    section.scroll === false ? 'flex flex-col overflow-hidden' : 'overflow-y-auto',
                  )}
                >
                  {section.children(section.id === selected, reporterFor(section.id))}
                </div>
              ) : null,
            )}
          </div>
        </section>
      )}
    </div>
  );
}
