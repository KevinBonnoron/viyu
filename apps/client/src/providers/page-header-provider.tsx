import { createContext, type ReactNode, useContext, useMemo } from 'react';

const PageHeaderContext = createContext<{ target: HTMLElement | null; compact: boolean } | undefined>(undefined);

export function PageHeaderProvider({ target, compact, children }: { target: HTMLElement | null; compact: boolean; children: ReactNode }) {
  const value = useMemo(() => ({ target, compact }), [target, compact]);
  return <PageHeaderContext.Provider value={value}>{children}</PageHeaderContext.Provider>;
}

export function usePageHeader() {
  return useContext(PageHeaderContext);
}
