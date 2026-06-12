// Shared display locale for storefront content (i18n). Empty = default locale.
// Backed by Nuxt useState so the header selector and pages stay in sync (SSR-safe).
export const useLocale = () => useState<string>('teggo-locale', () => '')
