import { createContext, type ReactNode, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { Snackbar } from '@/components/ui/snackbar';
import { useI18n } from '@/providers/i18n-provider';

/** How long an action can be taken back. */
const UNDO_MS = 6000;

interface UndoOffer {
  id: number;
  message: string;
  undo: () => Promise<void>;
}

type OfferUndo = (message: string, undo: () => Promise<void>) => void;

const UndoContext = createContext<OfferUndo | null>(null);

/**
 * One snackbar for the whole app that lets the last action be taken back for a few seconds. It lives above the pages, so it
 * stays when the card that triggered the action disappears (a video marked watched while watched videos are hidden).
 */
export function UndoProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [offer, setOffer] = useState<UndoOffer | null>(null);
  const [state, setState] = useState<'idle' | 'undoing' | 'failed'>('idle');
  const next = useRef(0);
  const offerUndo = useCallback<OfferUndo>((message, undo) => {
    next.current += 1;
    setState('idle');
    setOffer({ id: next.current, message, undo });
  }, []);
  useEffect(() => {
    if (!offer || state === 'undoing') {
      return;
    }
    const timer = window.setTimeout(() => setOffer((current) => (current?.id === offer.id ? null : current)), UNDO_MS);
    return () => window.clearTimeout(timer);
  }, [offer, state]);
  const undo = async () => {
    if (!offer || state === 'undoing') {
      return;
    }
    setState('undoing');
    try {
      await offer.undo();
      setOffer((current) => (current?.id === offer.id ? null : current));
      setState('idle');
    } catch {
      setState('failed');
    }
  };
  return (
    <UndoContext.Provider value={offerUndo}>
      {children}
      {offer && (
        <div className="fixed bottom-20 right-4 z-50 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2 md:bottom-4">
          <Snackbar
            tone="neutral"
            message={state === 'failed' ? t('video.historyActionError') : offer.message}
            action={t('common.undo')}
            disabled={state === 'undoing'}
            onAction={() => void undo()}
          />
        </div>
      )}
    </UndoContext.Provider>
  );
}

/** Offers to take back an action that just succeeded. */
export function useUndo(): OfferUndo {
  const context = useContext(UndoContext);
  if (!context) {
    throw new Error('useUndo must be used within UndoProvider');
  }
  return context;
}
