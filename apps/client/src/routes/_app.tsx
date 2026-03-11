import { createFileRoute, Outlet, redirect } from '@tanstack/react-router';
import { AppLayout } from '@/components/layout/app-layout';
import { ProfileTheme } from '@/components/profile-theme';
import { pb } from '@/lib/pocketbase';
import { PlaybackPrivacyProvider } from '@/providers/playback-privacy-provider';
import { ProfilesProvider } from '@/providers/profiles-provider';
import { SourcesProvider } from '@/providers/sources-provider';
import { SubscriptionsProvider } from '@/providers/subscriptions-provider';
import { UndoProvider } from '@/providers/undo-provider';

export const Route = createFileRoute('/_app')({
  beforeLoad: () => {
    if (!pb.authStore.isValid) {
      throw redirect({ to: '/login' });
    }
  },
  component: () => (
    <ProfilesProvider>
      <ProfileTheme />
      <SourcesProvider>
        <SubscriptionsProvider>
          <PlaybackPrivacyProvider>
            <UndoProvider>
              <AppLayout>
                <Outlet />
              </AppLayout>
            </UndoProvider>
          </PlaybackPrivacyProvider>
        </SubscriptionsProvider>
      </SourcesProvider>
    </ProfilesProvider>
  ),
});
