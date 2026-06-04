// Docusaurus babel-transpiles registered theme component directories, not just
// site `src/`. The OpenAPI theme (docusaurus-theme-openapi-docs) ships its theme
// components as tsc-compiled CommonJS (`require`/`exports`, no `import`/`export`).
// With babel's default `sourceType: 'module'`, babel treats those CJS files as ES
// modules and injects an ESM runtime-helper `import` (e.g. @babel/runtime/helpers/
// esm/createForOfIteratorHelperLoose). That lone `import` makes webpack compile the
// module as ESM ("harmony"), so the file's own `exports.* = …` references resolve to
// an undefined binding → "ReferenceError: exports is not defined" in the API explorer.
//
// `sourceType: 'unambiguous'` makes babel detect each file by its syntax instead:
// the CJS theme files are parsed as scripts (CJS helpers via `require`, no harmony),
// while Docusaurus's own generated ESM files (routes/registry) are parsed as modules.
module.exports = {
  presets: [require.resolve('@docusaurus/core/lib/babel/preset')],
  sourceType: 'unambiguous',
}
