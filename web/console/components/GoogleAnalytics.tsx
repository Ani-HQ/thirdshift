/**
 * GA4 loader. Measurement ID is baked in at build time via
 * NEXT_PUBLIC_GA_MEASUREMENT_ID (Next inlines public env on `next build`).
 *
 * Rendered as plain <script> tags in <head> so Google's tag detector (and
 * other non-JS crawlers) see the snippet in the initial HTML. next/script
 * afterInteractive only injects post-hydration, which fails GA "Retest".
 */
export function GoogleAnalytics() {
  const measurementId = process.env.NEXT_PUBLIC_GA_MEASUREMENT_ID?.trim();
  if (!measurementId) {
    return null;
  }
  return (
    <>
      <script async src={`https://www.googletagmanager.com/gtag/js?id=${measurementId}`} />
      <script
        id="google-analytics"
        dangerouslySetInnerHTML={{
          __html: `window.dataLayer = window.dataLayer || [];
function gtag(){dataLayer.push(arguments);}
gtag('js', new Date());
gtag('config', '${measurementId}');`
        }}
      />
    </>
  );
}
