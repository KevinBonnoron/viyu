import { app } from './server';

export default {
  port: 3000,
  hostname: '0.0.0.0',
  fetch(request: Request): Response | Promise<Response> {
    return app.fetch(request);
  },
  idleTimeout: 255,
};
