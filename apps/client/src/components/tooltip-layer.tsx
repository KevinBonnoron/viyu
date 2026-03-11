import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

export function TooltipLayer() {
  const id = useId();
  const bubble = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<{ target: HTMLElement; text: string; shortcut: string | null } | null>(null);
  const [position, setPosition] = useState({ left: 0, top: 0 });
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let target: HTMLElement | null = null;
    let text = '';
    let shortcut: string | null = null;
    let description: string | null = null;
    const close = () => {
      clearTimeout(timer);
      if (target) {
        if (!target.hasAttribute('title')) {
          target.setAttribute('title', text);
        }
        if (description === null) {
          target.removeAttribute('aria-describedby');
        } else {
          target.setAttribute('aria-describedby', description);
        }
      }
      target = null;
      setActive(null);
    };
    const show = (element: HTMLElement, keyboard: boolean) => {
      if (target === element) {
        return;
      }
      close();
      text = element.getAttribute('title')?.trim() ?? '';
      if (!text) {
        return;
      }
      target = element;
      shortcut = element.getAttribute('data-shortcut');
      description = element.getAttribute('aria-describedby');
      element.removeAttribute('title');
      element.setAttribute('aria-describedby', [description, id].filter(Boolean).join(' '));
      timer = setTimeout(() => setActive({ target: element, text, shortcut }), keyboard ? 0 : 120);
    };
    const enter = (event: Event) => {
      if (event instanceof PointerEvent && event.pointerType === 'touch') {
        return;
      }
      const element = event.target instanceof Element ? event.target.closest<HTMLElement>('[title]') : null;
      if (element) {
        show(element, event.type === 'focusin');
      }
    };
    const leave = (event: Event) => {
      const next = event instanceof MouseEvent || event instanceof FocusEvent ? event.relatedTarget : null;
      if (target && (!(next instanceof Node) || !target.contains(next))) {
        close();
      }
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        close();
      }
    };
    document.addEventListener('pointerover', enter, true);
    document.addEventListener('focusin', enter, true);
    document.addEventListener('pointerout', leave, true);
    document.addEventListener('focusout', leave, true);
    document.addEventListener('pointerdown', close, true);
    document.addEventListener('keydown', key, true);
    document.addEventListener('scroll', close, true);
    document.addEventListener('fullscreenchange', close);
    window.addEventListener('resize', close);
    return () => {
      close();
      document.removeEventListener('pointerover', enter, true);
      document.removeEventListener('focusin', enter, true);
      document.removeEventListener('pointerout', leave, true);
      document.removeEventListener('focusout', leave, true);
      document.removeEventListener('pointerdown', close, true);
      document.removeEventListener('keydown', key, true);
      document.removeEventListener('scroll', close, true);
      document.removeEventListener('fullscreenchange', close);
      window.removeEventListener('resize', close);
    };
  }, [id]);
  useLayoutEffect(() => {
    if (!active || !bubble.current) {
      return;
    }
    const anchor = active.target.getBoundingClientRect();
    const bounds = bubble.current.getBoundingClientRect();
    const left = Math.max(8, Math.min(anchor.left + anchor.width / 2 - bounds.width / 2, window.innerWidth - bounds.width - 8));
    const below = anchor.bottom + 8;
    const top = below + bounds.height <= window.innerHeight - 8 ? below : Math.max(8, anchor.top - bounds.height - 8);
    setPosition({ left, top });
  }, [active]);
  if (!active) {
    return null;
  }
  return createPortal(
    <div
      ref={bubble}
      id={id}
      role="tooltip"
      className="pointer-events-none fixed z-[150] max-w-[min(20rem,calc(100vw-1rem))] whitespace-pre-line rounded-lg border border-border bg-popover px-3 py-2 text-xs leading-relaxed text-popover-foreground shadow-lg"
      style={position}
    >
      <span className="whitespace-nowrap">
        {active.text}
        {active.shortcut && (
          <kbd className="ml-1.5 inline-flex min-w-5 items-center justify-center rounded border border-border bg-muted px-1 py-0.5 align-middle text-[10px] font-bold uppercase text-primary">
            {active.shortcut}
          </kbd>
        )}
      </span>
    </div>,
    document.fullscreenElement ?? document.body,
  );
}
