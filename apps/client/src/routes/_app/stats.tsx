import { createFileRoute } from '@tanstack/react-router';
import { StatsPage } from '@/components/stats-page';

export const Route = createFileRoute('/_app/stats')({ component: StatsPage });
