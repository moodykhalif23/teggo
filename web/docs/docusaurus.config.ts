import type * as Preset from '@docusaurus/preset-classic'
import type { Config } from '@docusaurus/types'
import type * as OpenApiPlugin from 'docusaurus-plugin-openapi-docs'

// Teggo developer documentation. Docs-only site (served at /); the API reference
// is generated from the OpenAPI 3.1 contract in @teggo/api so it never drifts
// from the generated TypeScript client.
const config: Config = {
  title: 'Teggo Developer Docs',
  tagline: 'Self-hosted, API-first B2B commerce platform',
  favicon: 'img/favicon.ico',
  url: 'https://docs.teggo.local',
  baseUrl: '/',
  organizationName: 'teggo',
  projectName: 'teggo',
  onBrokenLinks: 'warn',
  onBrokenMarkdownLinks: 'warn',

  i18n: { defaultLocale: 'en', locales: ['en'] },

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          docItemComponent: '@theme/ApiItem', // OpenAPI-aware doc item
        },
        blog: false,
        theme: { customCss: './src/css/custom.css' },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    // The OpenAPI theme bundles postman-code-generators for its language-tab
    // snippets, which reference Node core modules webpack 5 no longer polyfills.
    // We don't run those generators in the browser, so stub the modules out.
    function nodePolyfillFallback() {
      return {
        name: 'node-polyfill-fallback',
        configureWebpack() {
          return {
            resolve: {
              fallback: { path: false, fs: false, os: false, crypto: false, http: false, https: false, stream: false, zlib: false, util: false },
            },
          }
        },
      }
    },
    [
      'docusaurus-plugin-openapi-docs',
      {
        id: 'openapi',
        docsPluginId: 'classic',
        config: {
          teggo: {
            specPath: '../packages/api/openapi.yaml',
            outputDir: 'docs/api',
            downloadUrl: '/openapi.yaml',
            sidebarOptions: { groupPathsBy: 'tag', categoryLinkSource: 'tag' },
          } satisfies OpenApiPlugin.Options,
        },
      },
    ],
  ],

  themes: ['docusaurus-theme-openapi-docs'],

  themeConfig: {
    navbar: {
      title: 'Teggo',
      items: [
        { type: 'docSidebar', sidebarId: 'guides', position: 'left', label: 'Guides' },
        { type: 'docSidebar', sidebarId: 'api', position: 'left', label: 'API reference' },
      ],
    },
    footer: {
      style: 'dark',
      copyright: 'Teggo — self-hosted B2B commerce.',
    },
    prism: {
      additionalLanguages: ['go', 'bash', 'sql', 'json'],
    },
  } satisfies Preset.ThemeConfig,
}

export default config
