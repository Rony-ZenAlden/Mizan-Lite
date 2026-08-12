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

/**
 * Subtracts one minor-unit string from another, exactly.
 *
 * # Why BigInt and not Number
 *
 * This file exists because float64 loses integer precision above 2^53. Parsing these strings
 * with Number to do arithmetic would reintroduce exactly the defect the string representation
 * was chosen to avoid — and it would do so silently, on the largest amounts, which are the ones
 * a mistake costs most.
 *
 * BigInt is arbitrary-precision integer arithmetic, which is what minor units ARE. It is the
 * only correct tool in JavaScript for this, and it is available everywhere this application
 * runs.
 *
 * # What this may and may not be used for
 *
 * Change due at a till, which the operator needs the instant they type a tendered amount and
 * cannot wait for a round trip to learn. It is a figure for the drawer.
 *
 * It is NOT how a total is decided. Every amount that gets STORED is computed in Go and read
 * back — a number the frontend worked out is a number nobody can audit.
 */
export function subtractMinor(left: string, right: string): string {
  return (BigInt(left || "0") - BigInt(right || "0")).toString();
}

/** Compares two minor-unit strings exactly. Negative, zero, or positive, like a comparator. */
export function compareMinor(left: string, right: string): number {
  const difference = BigInt(left || "0") - BigInt(right || "0");
  if (difference < 0n) return -1;
  return difference > 0n ? 1 : 0;
}

/**
 * Parses a typed decimal amount into minor units, exactly.
 *
 * Digit manipulation on the STRING, never `parseFloat(value) * 100` — which turns 8.07 into
 * 806.9999999999999 and then, after rounding, into a till that is a penny out once a day and
 * nobody can say why.
 *
 * Returns null for anything that is not a plain amount, so a caller can refuse rather than
 * guess.
 */
export function parseMinor(value: string, decimals = 2): string | null {
  const trimmed = value.trim();
  if (!/^-?\d*(\.\d*)?$/.test(trimmed) || trimmed === "" || trimmed === "-") return null;

  const negative = trimmed.startsWith("-");
  const digits = negative ? trimmed.slice(1) : trimmed;
  const [whole, fraction = ""] = digits.split(".");

  // More typed decimals than the currency has is a mistake worth refusing, not truncating: a
  // till that quietly drops the third digit has taken a different amount than the one shown.
  if (fraction.length > decimals) return null;

  const minor = `${whole || "0"}${fraction.padEnd(decimals, "0")}`.replace(/^0+(?=\d)/, "");
  return negative && !/^0*$/.test(minor) ? `-${minor}` : minor;
}
