import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  agentRules: false,
  // Turbopack can reuse the same chunk filename across builds even when the
  // contents change, so content-hashed names alone don't guarantee that a
  // browser picks up a new deploy. deploymentId appends a per-build query
  // string to every static asset request, which does.
  deploymentId: process.env.ARUZOR_DEPLOYMENT_ID,

  // Proxy the API through the frontend's own origin. This is what lets one
  // published port serve the whole app: the browser only ever talks to the
  // frontend, so there is no second address to configure, no CORS to get
  // right, and nothing baked into the bundle that ties it to one hostname.
  // A reverse proxy in front (nginx, Caddy) handles /api itself and never
  // reaches this rule, so both deployment shapes work from one build.
  // Security headers for the HTML the browser actually loads. The API sets
  // its own headers in Go middleware, but those never reached the pages
  // served by Next.js — where the session token lives in localStorage, so an
  // injected script would be the thing to worry about. These close that gap
  // at the source, so every install gets them without touching nginx.
  //
  // The CSP keeps 'unsafe-inline' for scripts because Next.js's hydration
  // bootstrap is an inline script and moving to nonces needs middleware that
  // can break streaming; the rest of the policy (no external anything, no
  // framing, no plugins) still holds and is a large step up from no CSP.
  async headers() {
    // `next dev`'s Fast Refresh needs eval() at runtime; a production build
    // never calls it. Without this split, the strict CSP below makes every
    // component re-render throw in development while looking fine in prod.
    const scriptSrc = process.env.NODE_ENV === "development" ? "'self' 'unsafe-inline' 'unsafe-eval'" : "'self' 'unsafe-inline'";
    const csp = [
      "default-src 'self'",
      `script-src ${scriptSrc}`,
      "style-src 'self' 'unsafe-inline'",
      "img-src 'self' data:",
      "font-src 'self'",
      "connect-src 'self'",
      "manifest-src 'self'",
      "object-src 'none'",
      "base-uri 'self'",
      "form-action 'self'",
      "frame-ancestors 'none'",
    ].join("; ");
    return [
      {
        source: "/:path*",
        headers: [
          { key: "Content-Security-Policy", value: csp },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "same-origin" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
          // Tells browsers to stick to HTTPS for two years, subdomains
          // included. Harmless when Aruzor is reached over plain HTTP in
          // development — the header is simply ignored there.
          { key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains" },
        ],
      },
    ];
  },

  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.ARUZOR_BACKEND_URL ?? "http://localhost:8080"}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
