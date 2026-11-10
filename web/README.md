# dmsx console

Vite + React 19 + TypeScript admin UI. `npm run build` writes into `../internal/server/ui` for Go embed.

## Layout

```
src/
  api/           HTTP client and shared response types
  components/    Reusable UI pieces (badges, language switch)
  constants/     Nav sections and shared enums
  hooks/         Theme and app data
  i18n/          Locale provider + zh/en message catalogs
  layouts/       Auth gate and authenticated shell
  lib/           Formatting helpers
  pages/         One module per console section
  App.tsx        Session + section routing
  main.tsx       Providers
  styles.css     Theme tokens and layout
```

## Locales

Chinese (`zh`) and English (`en`). Choice is stored in `localStorage` as `dmsx.locale`, with browser language as the first-run default. Switch from the sidebar or the sign-in screen.

## Commands

```bash
npm install
npm run dev      # proxies /api to :8080
npm run build
```
