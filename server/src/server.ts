import { Hono } from 'hono';
import { cors } from 'hono/cors';
import { healthRoutes } from './routes';

export const app = new Hono()
  .basePath('/api')
  .use(
    '*',
    cors({
      origin: '*',
      allowMethods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS', 'PATCH'],
      allowHeaders: ['Content-Type', 'Authorization', 'X-Requested-With'],
    }),
  )
  .route('/health', healthRoutes);
