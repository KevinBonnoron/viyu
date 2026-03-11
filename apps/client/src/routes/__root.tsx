import { createRootRoute, Outlet } from '@tanstack/react-router';
import { RouteError, RouteNotFound } from '@/components/route-fallbacks';
import { AuthProvider } from '@/providers/auth-provider';
import { ThemeProvider } from '@/providers/theme-provider';

export const Route = createRootRoute({
  component: () => (
    <ThemeProvider>
      <AuthProvider>
        <Outlet />
      </AuthProvider>
    </ThemeProvider>
  ),
  notFoundComponent: RouteNotFound,
  errorComponent: RouteError,
});
