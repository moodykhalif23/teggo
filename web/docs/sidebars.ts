import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

// Two sidebars: hand-authored developer guides, and the API reference generated
// by the OpenAPI plugin (docs/api/sidebar.ts is produced by `pnpm gen-api`).
const sidebars: SidebarsConfig = {
  guides: [
    'intro',
    'getting-started',
    'architecture',
    'conventions',
    'module-pattern',
    'auth-rbac',
    'data-layer',
    'background-jobs',
    'integrations',
    'frontend',
    'configuration',
  ],
  api: [
    {
      type: 'category',
      label: 'API reference',
      link: { type: 'generated-index', title: 'Teggo API', slug: '/api' },
      items: require('./docs/api/sidebar.ts'),
    },
  ],
}

export default sidebars
