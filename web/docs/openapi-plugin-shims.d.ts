// Ambient shims for docusaurus-plugin-openapi-docs@4.4.0.
// These are internal sidebar types the plugin uses for navigation generation; none of
// them feed the `Options` fields docusaurus.config.ts validates via `satisfies`, so
// declaring them as `unknown` resolves the imports without loosening any checking we
// rely on. Remove this file if a future plugin/Docusaurus release fixes the imports.
declare module '@docusaurus/plugin-content-docs-types' {
  export type SidebarItemLink = unknown
  export type PropSidebarItemCategory = unknown
  export type PropSidebarItem = unknown
  export type PropSidebar = unknown
  export type PropSidebars = unknown
}

declare module '@docusaurus/plugin-content-docs/src/sidebars/types' {
  export type SidebarItemDoc = unknown
}
