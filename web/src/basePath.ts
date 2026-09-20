// Read once at module load, shared by src/api/client.ts (prefixes every
// fetch) and src/main.tsx (React Router's basename) -- see
// internal/api/static.go's rewriteIndexHTML for where this tag comes
// from. "" (default, when the tag is absent) means "no prefix", which is
// always the case in local dev (`npm run dev` serves the raw,
// un-rewritten index.html) and in a production build served at the
// domain root.
//
// A <meta> tag, not window.__SOMETHING__ set via an inline <script> (an
// earlier version of this used one) -- deliberately, so a strict
// script-src 'self' CSP (see internal/api/security_headers.go) never
// needs a nonce or 'unsafe-inline' carved out just for this one value;
// reading a <meta> tag isn't script execution, CSP's script-src has no
// opinion on it either way.
export const BASE_PATH = document.querySelector('meta[name="am4bot-base-path"]')?.getAttribute('content') ?? ''
