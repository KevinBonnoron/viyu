import { defineConfig } from 'vitepress';

export default defineConfig({
  title: 'template',
  description: 'Template',

  base: '/template/',

  vite: {
    server: { port: 5000 },
  },

  themeConfig: {
    nav: [{ text: 'Guide', link: '/' }],

    sidebar: [
      {
        text: 'Guide',
        items: [],
      },
    ],

    socialLinks: [{ icon: 'github', link: 'https://github.com/KevinBonnoron/template' }],

    footer: {
      message: 'Released under the MIT License.',
    },
  },
});
