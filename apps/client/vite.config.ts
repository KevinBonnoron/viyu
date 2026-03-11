import path from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import tailwindcss from '@tailwindcss/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'VIYU_');
  const target = env.VIYU_SERVER_URL || 'http://127.0.0.1:8090';

  return {
    plugins: [
      {
        name: 'viyu-backend-readiness',
        apply: 'serve',
        async configureServer(server) {
          const health = new URL('/api/health', target);
          const deadline = Date.now() + 120_000;
          server.config.logger.info(`Waiting for Go/PocketBase at ${target}…`);
          while (Date.now() < deadline) {
            try {
              const response = await fetch(health, { signal: AbortSignal.timeout(1000) });
              const body: unknown = await response.json();
              if (response.ok && typeof body === 'object' && body !== null && 'status' in body && body.status === 'ok') {
                return;
              }
            } catch {
              // Refused connections are expected while task dev compiles Go.
            }
            await delay(250);
          }
          throw new Error(`Go/PocketBase did not become ready at ${target}. Start task server:dev and check its output.`);
        },
      },
      // The router plugin must run before the React plugin.
      tanstackRouter({
        target: 'react',
        autoCodeSplitting: true,
      }),
      react(),
      tailwindcss(),
    ],
    resolve: {
      alias: {
        '@': path.resolve(import.meta.dirname, './src'),
      },
    },
    server: {
      proxy: {
        '/api': { target, changeOrigin: true },
        '/_': { target, changeOrigin: true },
      },
    },
  };
});
