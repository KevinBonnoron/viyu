import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useI18n } from '@/providers/i18n-provider';

export type ChronologicalOrder = 'desc' | 'asc';

export function ChronologicalOrderSelect({ value, onChange }: { value: ChronologicalOrder; onChange: (order: ChronologicalOrder) => void }) {
  const { t } = useI18n();
  return (
    <Select
      value={value}
      onValueChange={(next) => {
        if (next === 'desc' || next === 'asc') {
          onChange(next);
        }
      }}
    >
      <SelectTrigger aria-label={t('common.sortOrder')} className="w-40 shrink-0">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="desc">{t('common.newestFirst')}</SelectItem>
        <SelectItem value="asc">{t('common.oldestFirst')}</SelectItem>
      </SelectContent>
    </Select>
  );
}
