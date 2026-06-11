import type * as Preset from '@docusaurus/preset-classic'
import type { Config } from '@docusaurus/types'
import type * as OpenApiPlugin from 'docusaurus-plugin-openapi-docs'

// Teggo developer documentation. Docs-only site (served at /); the API reference
// is generated from the OpenAPI 3.1 contract in @teggo/api so it never drifts
// from the generated TypeScript client.

// Where "Get started" sends people: the admin dashboard of the deployment this
// site fronts. Override per environment (e.g. https://admin.teggo.example).
const ADMIN_URL = process.env.TEGGO_ADMIN_URL ?? 'http://localhost:5173'

const config: Config = {
  title: 'Teggo Developer Docs',
  tagline: 'Self-hosted, API-first B2B commerce platform',
  favicon: 'img/favicon.svg',
  url: 'https://docs.teggo.local',
  baseUrl: '/',
  organizationName: 'teggo',
  projectName: 'teggo',
  onBrokenLinks: 'warn',
  onBrokenMarkdownLinks: 'warn',

  i18n: { defaultLocale: 'en', locales: ['en'] },

  customFields: { adminUrl: ADMIN_URL },

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
    // Also silence one benign warning: the generated API info page imports the
    // theme's SchemaTabs, which ships as a CJS module (`exports.default`); on the
    // client compile webpack's static analysis surfaces only `__esModule` and warns
    // "export 'default' ... was not found in '@theme/SchemaTabs'". The component
    // renders correctly — it's a CJS/ESM interop notice, not a real missing export.
    function webpackTweaks() {
      return {
        name: 'webpack-tweaks',
        configureWebpack() {
          return {
            resolve: {
              fallback: { path: false, fs: false, os: false, crypto: false, http: false, https: false, stream: false, zlib: false, util: false },
            },
            ignoreWarnings: [
              (warning: { message?: string }) =>
                /export 'default'.*SchemaTabs.*was not found/.test(warning?.message ?? ''),
            ],
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

  // Universal product font — Open Sans, matching admin/storefront/vendor.
  headTags: [
    { tagName: 'link', attributes: { rel: 'preconnect', href: 'https://fonts.googleapis.com' } },
    {
      tagName: 'link',
      attributes: { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: 'true' },
    },
  ],
  stylesheets: [
    'https://fonts.googleapis.com/css2?family=Open+Sans:ital,wght@0,300..800;1,300..800&display=swap',
  ],

  themeConfig: {
    navbar: {
      title: 'Teggo',
      logo: { alt: 'Teggo', src: 'img/logo.svg' },
      items: [
        { type: 'docSidebar', sidebarId: 'guides', position: 'left', label: 'Guides' },
        { type: 'docSidebar', sidebarId: 'api', position: 'left', label: 'API reference' },
        { href: ADMIN_URL, label: 'Get started', position: 'right', className: 'navbar-cta' },
      ],
    },
    // Light, minimal footer — a single row of links, no developer-guide columns.
    footer: {
      style: 'light',
      links: [
        { label: 'Admin dashboard', href: ADMIN_URL },
        { label: 'Documentation', to: '/intro' },
        { label: 'API reference', to: '/api' },
      ],
      copyright: `© ${new Date().getFullYear()} Teggo — self-hosted B2B commerce.`,
    },
    prism: {
      additionalLanguages: ['go', 'bash', 'sql', 'json'],
    },
  } satisfies Preset.ThemeConfig,
}

export default config
