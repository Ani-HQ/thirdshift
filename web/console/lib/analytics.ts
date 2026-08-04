type Gtag = (...args: unknown[]) => void;

declare global {
  interface Window {
    gtag?: Gtag;
    dataLayer?: unknown[];
  }
}

/** Fire a GA4 event when the measurement ID is present in the built bundle. */
export function trackEvent(name: string, params?: Record<string, string | number | boolean | undefined>) {
  if (typeof window === "undefined" || typeof window.gtag !== "function") {
    return;
  }
  window.gtag("event", name, params);
}
