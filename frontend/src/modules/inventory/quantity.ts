/**
 * Quantities cross the boundary as STRINGS of micro units — integers scaled by 10⁶.
 *
 * Formatted here by string manipulation, never by `Number()`. The reason is the one that put them
 * in a string to begin with: JavaScript's number type is a float64, and a wholesaler's quantity
 * in micro units passes 2^53 at nine billion units. Parsing to format would reintroduce exactly
 * the imprecision the string was chosen to avoid.
 *
 * This is the same treatment `modules/accounting/money.ts` gives minor units, and it is
 * deliberately a second small module rather than a shared one: a quantity has six implied
 * decimals and a currency has however many its definition says, and merging them would mean one
 * function that has to be told which it is looking at.
 */

const QUANTITY_DECIMALS = 6;

/**
 * Formats a micro quantity for display, trimming the trailing zeros nobody wants to read.
 *
 * "10000000" → "10", "2500000" → "2.5", "1437000" → "1.437".
 */
export function formatQuantity(micro: string, maxDecimals = QUANTITY_DECIMALS): string {
  const negative = micro.startsWith("-");
  const digits = (negative ? micro.slice(1) : micro).replace(/^0+(?=\d)/, "");

  const padded = digits.padStart(QUANTITY_DECIMALS + 1, "0");
  const whole = padded.slice(0, padded.length - QUANTITY_DECIMALS);
  let fraction = padded.slice(padded.length - QUANTITY_DECIMALS);

  // Trim to the requested precision, then drop trailing zeros: "2.500000" reads as "2.5", and a
  // shelf label that says "2.500000" tells nobody anything the shorter one does not.
  fraction = fraction.slice(0, maxDecimals).replace(/0+$/, "");

  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const body = fraction ? `${grouped}.${fraction}` : grouped;
  return negative && body !== "0" ? `-${body}` : body;
}

/** Reports whether a micro quantity is zero, without parsing it. */
export function isZeroQuantity(micro: string): boolean {
  return /^-?0*$/.test(micro.replace(/[.,]/g, ""));
}

/** Reports whether a micro quantity is negative — which stock genuinely can be. */
export function isNegativeQuantity(micro: string): boolean {
  return micro.startsWith("-") && !isZeroQuantity(micro);
}

/**
 * Turns what somebody typed into micro units, without going through a float.
 *
 * "2.5" → "2500000". Returns null for anything that is not a plain decimal, so a caller shows a
 * validation message rather than sending NaN to the backend.
 */
export function toMicro(entered: string): string | null {
  const trimmed = entered.trim();
  if (!/^-?\d*(\.\d*)?$/.test(trimmed) || trimmed === "" || trimmed === "-") return null;

  const negative = trimmed.startsWith("-");
  const [whole, fraction = ""] = (negative ? trimmed.slice(1) : trimmed).split(".");
  if (fraction.length > QUANTITY_DECIMALS) return null;

  const digits = `${whole || "0"}${fraction.padEnd(QUANTITY_DECIMALS, "0")}`;
  const normalised = digits.replace(/^0+(?=\d)/, "");
  return negative && normalised !== "0" ? `-${normalised}` : normalised;
}
