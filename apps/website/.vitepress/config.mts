import { defineConfig } from 'vitepress';

export default defineConfig({
  title: 'Viyu',
  description: 'Self-hosted video subscriptions and player for YouTube, Twitch and PeerTube',

  base: '/viyu/',

  vite: {
    server: { port: 5000 },
  },

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/' },
      { text: 'Running a server', link: '/guide/self-hosting' },
      { text: 'Architecture decisions', link: 'https://github.com/KevinBonnoron/viyu/tree/main/docs/adr' },
    ],

    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Introduction', link: '/' },
          { text: 'Sources', link: '/guide/sources' },
          { text: 'Watching', link: '/guide/watching' },
          { text: 'Library', link: '/guide/library' },
          { text: 'Settings and accounts', link: '/guide/settings' },
        ],
      },
      {
        text: 'Operating Viyu',
        items: [
          { text: 'Privacy and security', link: '/guide/privacy' },
          { text: 'Running a server', link: '/guide/self-hosting' },
        ],
      },
    ],

    socialLinks: [{ icon: 'github', link: 'https://github.com/KevinBonnoron/viyu' }],

    footer: {
      message: 'Released under the MIT License.',
    },
  },
});
