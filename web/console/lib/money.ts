/**
 * Formats a host credit for the public ticker.
 *
 * Early network earnings are genuinely tiny — a few microdollars — and
 * rounding them to cents would print "$0.00" next to a machine that really did
 * earn something. So below a cent we keep up to microdollar precision and trim
 * trailing zeros; at a cent and above we switch to ordinary money with two
 * decimals and thousands separators.
 */
export function formatEarnings(microdollars: number): string {
  if (!Number.isFinite(microdollars) || microdollars <= 0) {
    return "$0.00";
  }
  const dollars = microdollars / 1_000_000;
  if (dollars >= 0.01) {
    return `$${dollars.toLocaleString("en-US", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2
    })}`;
  }
  return `$${dollars.toFixed(6).replace(/0+$/, "")}`;
}
