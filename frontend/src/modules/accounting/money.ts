/**
 * Formats a minor-unit amount that crossed the boundary as a string.
 *
 * # Why the amount is text and stays text
 *
 * JavaScript numbers are float64 and lose integer precision above 2^53. That ceiling is far
 * away for a currency with two decimals and much nearer for one with none — a hyperinflated
 * currency reaches it in ordinary trading, which is exactly the situation this product is built
 * for (§18).
 *
 * So money crosses as text and is FORMATTED here, never added up. Every total worth trusting is
 * computed in Go, where it is an exact integer.
 */
export function formatMinor(minor: string, decimals = 2): string {
  const negative = minor.startsWith("-");
  const digits = negative ? minor.slice(1) : minor;

  const padded = digits.padStart(decimals + 1, "0");
  const whole = padded.slice(0, padded.length - decimals) || "0";
  const fraction = decimals > 0 ? padded.slice(padded.length - decimals) : "";

  // Grouped by threes. Done on the STRING rather than by Number.toLocaleString, which would
  // mean parsing it into the float this whole approach exists to avoid.
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const body = decimals > 0 ? `${grouped}.${fraction}` : grouped;
  return negative ? `-${body}` : body;
}

/** Whether a minor-unit string is zero, without parsing it. */
export function isZeroMinor(minor: string): boolean {
  return /^-?0*$/.test(minor);
}
