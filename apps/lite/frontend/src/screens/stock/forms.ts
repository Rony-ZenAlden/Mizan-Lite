import { BindingError } from "@/api/envelope";
import { normaliseNumber, quantityProblem } from "@/i18n/numbers";

/**
 * Where a refusal from Go belongs on a form: under the field it names, or above the buttons when it names none.
 */
export function formErrors(error: unknown, errorText: (e: unknown) => string) {
  const fields = error instanceof BindingError ? (error.apiError.fields ?? []) : [];
  return {
    field: (name: string) => (fields.some((f) => f.field === name) ? errorText(error) : undefined),
    form: error && fields.length === 0 ? errorText(error) : null,
  };
}

/** The code a typed quantity would be refused with, or null — nothing is said about an empty field until it is typed in. */
export function typedQuantity(raw: string, decimals: number): string | null {
  return raw === "" ? null : quantityProblem(raw, decimals);
}

/** The code a typed amount (a cost, a rate) would be refused with, or null. Go checks its decimals. */
export function typedAmount(raw: string): string | null {
  if (raw === "") return null;
  const typed = normaliseNumber(raw);
  return typed.ok ? null : typed.code;
}
