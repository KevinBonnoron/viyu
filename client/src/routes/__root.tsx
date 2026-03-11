import { ThemeProvider } from '@/providers/theme-provider';
import { createRootRoute, Outlet } from '@tanstack/react-router';
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools';

export const Route = createRootRoute({
  component: () => (
    <ThemeProvider>
      <Outlet />
      <TanStackRouterDevtools />
    </ThemeProvider>
  ),
});
