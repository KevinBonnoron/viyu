import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.viyu.app',
  appName: 'Viyu',
  webDir: 'dist',
  server: {
    androidScheme: 'https',
  },
  android: {
    path: '../android',
    // The app is served from https://localhost; an http:// Viyu server would otherwise be blocked as mixed content.
    allowMixedContent: true,
  },
};

export default config;
