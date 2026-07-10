import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// GitHub Pages project site lives at https://srjn45.github.io/spaiSH
export default defineConfig({
  site: 'https://srjn45.github.io',
  base: '/spaiSH/',
  integrations: [
    starlight({
      title: 'spai',
      description: 'A Claude-Code-style AI agent for your terminal. Ask in plain language — spai reads files, runs commands, and edits code, behind a permission gate.',
      logo: {
        light: './src/assets/spai-wordmark-light.svg',
        dark: './src/assets/spai-wordmark-dark.svg',
        replacesTitle: true,
      },
      favicon: '/favicon.svg',
      customCss: ['./src/styles/docs.css'],
      head: [
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://srjn45.github.io/spaiSH/og-image.svg' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
      ],
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/srjn45/spaiSH' },
      ],
      sidebar: [
        { label: 'Start here', items: [
          { label: 'What is spai?', slug: 'start/what-is-spai' },
          { label: 'Install & setup', slug: 'start/install' },
          { label: 'Quickstart', slug: 'start/quickstart' },
        ]},
        { label: 'Guides', items: [
          { label: 'Interactive session', slug: 'guides/interactive-session' },
          { label: 'Permissions & modes', slug: 'guides/permissions' },
          { label: 'MCP servers', slug: 'guides/mcp' },
          { label: 'Local models', slug: 'guides/local-models' },
          { label: 'Shell integration', slug: 'guides/shell-integration' },
        ]},
        { label: 'Concepts', items: [
          { label: 'Architecture', slug: 'concepts/architecture' },
          { label: 'Vision', slug: 'concepts/vision' },
        ]},
        { label: 'Reference', items: [
          { label: 'Configuration', slug: 'reference/configuration' },
          { label: 'Roadmap', slug: 'reference/roadmap' },
          { label: 'Contributing', slug: 'reference/contributing' },
          { label: 'Legal', slug: 'reference/legal' },
        ]},
      ],
    }),
  ],
});
