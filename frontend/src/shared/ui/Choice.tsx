import { cn } from "./cn";

export interface Choice {
  value: string;
  label: string;
  /** One short line under the label. Optional, and usually worth writing. */
  hint?: string;
}

/**
 * A question answered by picking one of a few visible options.
 *
 * # Why not a Select
 *
 * A dropdown hides its options until asked and shows one at a time. That is right for twelve
 * months or two hundred currencies; it is wrong for "what kind of shop is this?", where the
 * options ARE the explanation. Somebody setting up for the first time does not know what a
 * "business profile" is, and a closed dropdown labelled that way tells them nothing — while four
 * cards reading *Retail shop*, *Restaurant*, *Wholesale*, *Workshop* answer the question by
 * existing.
 *
 * So the rule is about the reader, not the count: use this when seeing the alternatives is part
 * of understanding the question, and a Select when the user already knows what they are looking
 * for.
 *
 * # Radio semantics, not buttons
 *
 * A group of buttons is unnavigable by keyboard as a group and unannounced as a choice. These
 * are real radios with a real fieldset, so arrow keys move within the group, Tab leaves it, and
 * a screen reader says "2 of 4". The visual is a card; the mechanics are a radio.
 */
export function ChoiceGroup({
  legend,
  hint,
  value,
  onChange,
  choices,
  columns = 2,
  name,
}: {
  legend: string;
  hint?: string;
  value: string;
  onChange: (value: string) => void;
  choices: Choice[];
  columns?: 1 | 2 | 3;
  /** Groups radios together. Must be unique on the page. */
  name: string;
}) {
  return (
    <fieldset className="flex flex-col gap-2 border-0 p-0">
      <legend className="text-sm font-medium text-text">{legend}</legend>
      {hint ? <p className="text-xs text-text-muted">{hint}</p> : null}

      <div
        className={cn(
          "mt-1 grid gap-2",
          columns === 1 && "grid-cols-1",
          columns === 2 && "grid-cols-1 sm:grid-cols-2",
          columns === 3 && "grid-cols-1 sm:grid-cols-3",
        )}
      >
        {choices.map((choice) => {
          const selected = choice.value === value;
          return (
            <label
              key={choice.value}
              className={cn(
                "flex cursor-pointer items-start gap-2 rounded-xl border p-3",
                "focus-within:outline focus-within:outline-2 focus-within:outline-offset-2",
                "focus-within:outline-ring",
                selected
                  ? "border-primary bg-primary-subtle"
                  : "border-border bg-surface hover:border-border-strong",
              )}
            >
              <input
                type="radio"
                name={name}
                value={choice.value}
                checked={selected}
                onChange={() => onChange(choice.value)}
                // The native control carries the semantics and the focus; the card carries the
                // look. `sr-only` rather than `hidden`, which would remove it from the tab order
                // and make the whole group unreachable by keyboard.
                className="sr-only"
              />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-sm font-medium text-text">{choice.label}</span>
                {choice.hint ? (
                  <span className="text-xs text-text-muted">{choice.hint}</span>
                ) : null}
              </span>
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}
