import type { ReactNode } from 'react';
import { Brand } from '@/components/brand';
import { useI18n } from '@/providers/i18n-provider';

export function AuthLayout({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const { t } = useI18n();
  return (
    <main className="grid min-h-dvh lg:grid-cols-2">
      <aside className="hidden flex-col border-r bg-sidebar px-12 py-10 lg:flex xl:px-20">
        <Brand />
        <div className="my-auto max-w-md py-16">
          <p className="mb-5 text-xs font-medium uppercase tracking-widest text-primary">{t('ui.library.personalSpace')}</p>
          <h2 className="text-4xl font-semibold leading-tight tracking-tight xl:text-5xl">{t('ui.auth.tagline')}</h2>
          <p className="mt-5 max-w-sm text-base leading-relaxed text-muted-foreground">{t('ui.auth.description')}</p>
        </div>
      </aside>
      <div className="flex items-center justify-center px-6 py-12 sm:px-10">
        <div className="w-full max-w-sm">
          <div className="mb-10 lg:hidden">
            <Brand />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="mb-8 mt-2 text-sm leading-relaxed text-muted-foreground">{description}</p>
          {children}
        </div>
      </div>
    </main>
  );
}
