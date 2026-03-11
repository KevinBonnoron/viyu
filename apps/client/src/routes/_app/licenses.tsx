import { createFileRoute } from '@tanstack/react-router';
import { PageTitle } from '@/components/page-title';
import { useI18n } from '@/providers/i18n-provider';
import licenseText from '../../../../../LICENSE?raw';

export const Route = createFileRoute('/_app/licenses')({ component: LicensesPage });

function LicensesPage() {
  const { t } = useI18n();
  return (
    <div className="w-full space-y-7">
      <PageTitle description={t('licenses.description')}>{t('licenses.title')}</PageTitle>
      <section className="space-y-4 rounded-2xl border bg-card p-5 sm:p-6" aria-labelledby="sponsorblock-license">
        <h2 id="sponsorblock-license" className="text-base font-semibold">
          SponsorBlock
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">{t('licenses.sponsorblock')}</p>
        <div className="flex flex-wrap gap-x-5 gap-y-2 text-sm">
          <a href="https://sponsor.ajay.app/" target="_blank" rel="noreferrer" className="text-primary underline">
            SponsorBlock
          </a>
          <a href="https://creativecommons.org/licenses/by-nc-sa/4.0/" target="_blank" rel="noreferrer" className="text-primary underline">
            CC BY-NC-SA 4.0
          </a>
        </div>
      </section>
      <section className="space-y-4 rounded-2xl border bg-card p-5 sm:p-6" aria-labelledby="return-dislike-credit">
        <h2 id="return-dislike-credit" className="text-base font-semibold">
          Return YouTube Dislike
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">{t('licenses.returnDislike')}</p>
        <a href="https://returnyoutubedislike.com/" target="_blank" rel="noreferrer" className="text-sm text-primary underline">
          Return YouTube Dislike
        </a>
      </section>
      <section className="space-y-4 rounded-2xl border bg-card p-5 sm:p-6" aria-labelledby="viyu-license">
        <h2 id="viyu-license" className="text-base font-semibold">
          Viyu · MIT
        </h2>
        <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-muted-foreground">{licenseText}</pre>
      </section>
    </div>
  );
}
