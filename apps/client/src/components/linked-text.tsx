import { Fragment, type ReactNode } from 'react';
import { splitLinks } from '@/lib/text-links';

/** A link to another site: it opens in a new tab and tells that site nothing about this one. */
export function ExternalLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer nofollow"
      className="rounded-sm text-primary underline decoration-primary/40 underline-offset-2 hover:decoration-primary"
    >
      {children}
    </a>
  );
}

/**
 * A text whose web addresses are links. The rest is shown as it is, or through `renderText` when something else (the
 * mentions of a chat) has to be picked out of it.
 */
export function LinkedText({ text, renderText }: { text: string; renderText?: (plain: string) => ReactNode }) {
  return (
    <>
      {splitLinks(text).map((part) =>
        part.href ? (
          <ExternalLink key={part.offset} href={part.href}>
            {part.text}
          </ExternalLink>
        ) : (
          <Fragment key={part.offset}>{renderText ? renderText(part.text) : part.text}</Fragment>
        ),
      )}
    </>
  );
}
