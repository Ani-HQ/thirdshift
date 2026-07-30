import type { PublicCatalogModel } from "./types";

/**
 * Formats microdollars as a per-million-token price. Two decimals is the
 * normal case; a third is used when rounding to cents would misstate the
 * number (for example $0.015).
 */
export function formatPricePerMillion(microdollars: number): string {
  const dollars = microdollars / 1_000_000;
  const cents = dollars * 100;
  const needsThird = Math.abs(cents - Math.round(cents)) > 1e-9;
  return `$${dollars.toFixed(needsThird ? 3 : 2)}`;
}

/**
 * Discount against the operator-recorded typical hosted price, as the average
 * of the input and output discounts rounded to the nearest 5 percent. Returns
 * null when the manifest carries no comparison or when we are not actually
 * cheaper, so the page can never claim a saving it cannot support.
 */
export function comparisonDiscountPercent(model: PublicCatalogModel): number | null {
  const comparison = model.market_comparison;
  if (!comparison) {
    return null;
  }
  const typicalInput = comparison.typical_input_per_million_microdollars;
  const typicalOutput = comparison.typical_output_per_million_microdollars;
  if (typicalInput <= 0 || typicalOutput <= 0) {
    return null;
  }
  const inputDiscount = 1 - model.price.input_per_million_microdollars / typicalInput;
  const outputDiscount = 1 - model.price.output_per_million_microdollars / typicalOutput;
  const rounded = roundToNearestFive(((inputDiscount + outputDiscount) / 2) * 100);
  return rounded > 0 ? rounded : null;
}

/**
 * Half-up rounding to the nearest 5. The epsilon keeps exact halves such as
 * 22.5 from falling to the lower step through binary floating point drift.
 */
function roundToNearestFive(percent: number): number {
  return Math.floor(percent / 5 + 0.5 + 1e-9) * 5;
}

export function formatComparisonLine(comparison: PublicMarketComparisonLike): string {
  return `Typical hosted: ${formatPricePerMillion(comparison.typical_input_per_million_microdollars)} / ${formatPricePerMillion(
    comparison.typical_output_per_million_microdollars
  )}`;
}

type PublicMarketComparisonLike = {
  typical_input_per_million_microdollars: number;
  typical_output_per_million_microdollars: number;
};
