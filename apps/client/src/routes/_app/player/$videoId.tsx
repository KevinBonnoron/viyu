import { createFileRoute, useLocation } from '@tanstack/react-router';
import { RecordPlayer } from '@/features/video/components/record-player';
import { parseTimecode } from '@/features/video/utils/timecode';

export const Route = createFileRoute('/_app/player/$videoId')({
  validateSearch: (search: Record<string, unknown>): { series?: string; t?: number } => ({
    series: typeof search.series === 'string' && search.series ? search.series : undefined,
    // A shared link may carry a start time (`?t=90` or `?t=1m30s`); it wins over resume positions.
    t: parseTimecode(search.t as string | number | undefined),
  }),
  component: PlayerPage,
});

/** `videoId` is a `videos` record id. Content of a connected source carries that source in the navigation state: it is read privately through it. */
function PlayerPage() {
  const { videoId } = Route.useParams();
  const { series, t: startAt } = Route.useSearch();
  const source = useLocation({ select: (location) => location.state.viyuSource });
  return <RecordPlayer videoId={videoId} source={source} series={series} startAt={startAt} />;
}
