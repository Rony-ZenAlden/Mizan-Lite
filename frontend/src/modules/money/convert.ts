/**
 * Converting money in the browser, without ever making it a number.
 *
 * # Why this is done here at all
 *
 * A shop trading in a currency that moves wants to see both figures at once: the price in the
 * money the customer pays in, and what it is worth in the money the shop thinks in. Asking the
 * backend per row would be a round trip per line on a till — so the RATE crosses the boundary
 * once and the arithmetic happens here.
 *
 * # Why BigInt, and why this is still exact
 *
 * Everything money-shaped in Mizan crosses as a decimal string of an exact integer: amounts in
 * minor units, rates scaled by 10⁹. `Number` holds integers exactly only up to 2^53, and a
 * Syrian pound amount multiplied by a rate in the thousands passes that at a few hundred
 * thousand pounds — long before anything on screen looks wrong.
 *
 * BigInt has no upper bound and no rounding. The only rounding here is the FINAL division, and
 * it is done explicitly, half-up, in one place.
 *
 * # This is a DISPLAY conversion and nothing else
 *
 * Nothing posted, nothing stored, nothing summed into a ledger. Every figure a document is
 * actually written with is converted on the Go side at the rate the document snapshots (§9.3).
 * If these two ever disagree it is this one that is wrong, which is why it is never the number
 * anything is decided on.
 */

/** The scale a rate is stored at: rate × 10⁹. */
const RATE_SCALE = 1_000_000_000n;

/**
 * Converts an amount in minor units by a rate, returning minor units.
 *
 * Both in and out are decimal STRINGS of integers, so the value never passes through `Number`.
 * Returns null when either input is not an integer string — a caller showing nothing is correct,
 * where a caller showing NaN or zero would be lying.
 */
export function convertMinor(amountMinor: string, rateNano: string): string | null {
  const amount = toBigInt(amountMinor);
  const rate = toBigInt(rateNano);
  if (amount === null || rate === null || rate <= 0n) return null;

  const scaled = amount * rate;

  // Half-up, away from zero, applied to the absolute value so a negative converts symmetrically.
  // Rounding toward zero would make a refund of -0.5 land on a different figure from the sale of
  // 0.5 it reverses, and the two would stop cancelling.
  const negative = scaled < 0n;
  const magnitude = negative ? -scaled : scaled;
  const rounded = (magnitude + RATE_SCALE / 2n) / RATE_SCALE;
  return (negative ? -rounded : rounded).toString();
}

/** Parses a decimal integer string, or null. Rejects anything with a fractional part. */
function toBigInt(value: string): bigint | null {
  const trimmed = value.trim();
  if (trimmed === "" || !/^-?\d+$/.test(trimmed)) return null;
  try {
    return BigInt(trimmed);
  } catch {
    return null;
  }
}

/**
 * Converts an amount by DIVIDING by a rate, returning minor units.
 *
 * A shop records "one dollar buys 15,000 pounds", because that is the number it is quoted. So
 * showing what a pound amount is worth in dollars is a division by that rate.
 *
 * # Why this is not "invert the rate, then multiply"
 *
 * That was the first version and it was wrong. Inverting produces 10¹⁸ / rateNano, which
 * TRUNCATES — and the error is then multiplied by the amount. At a rate of 15,000 the inverted
 * rate loses precision in the ninth digit, which is invisible on one line of a receipt and
 * accumulates across a page of them.
 *
 * Dividing once, at the end, has exactly one rounding step. Same half-up rule as above, applied
 * to the magnitude so a refund converts symmetrically with the sale it reverses.
 */
export function divideMinor(amountMinor: string, rateNano: string): string | null {
  const amount = toBigInt(amountMinor);
  const rate = toBigInt(rateNano);
  if (amount === null || rate === null || rate <= 0n) return null;

  const scaled = amount * RATE_SCALE;
  const negative = scaled < 0n;
  const magnitude = negative ? -scaled : scaled;
  const rounded = (magnitude + rate / 2n) / rate;
  return (negative ? -rounded : rounded).toString();
}
