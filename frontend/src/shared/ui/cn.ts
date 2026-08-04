/**
 * Joins class names, dropping falsy entries.
 *
 * Deliberately not clsx + tailwind-merge. Those solve "a caller's class must override the
 * component's", which is a problem a design system should not have: a primitive that can be
 * arbitrarily restyled from the outside is no longer a primitive, and conflicting-class
 * resolution is how a token system quietly stops being the source of truth. Callers pass
 * layout classes (margins, width); variants come from props.
 */
export function cn(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}
