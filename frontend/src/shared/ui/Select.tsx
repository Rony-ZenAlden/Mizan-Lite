import * as RadixSelect from "@radix-ui/react-select";
import { useId } from "react";
import { cn } from "./cn";

export interface SelectOption {
  value: string;
  label: string;
}

export interface SelectProps {
  value: string;
  onValueChange: (value: string) => void;
  options: SelectOption[];
  label?: string;
  disabled?: boolean;
  /** Accessible name when no visible label is rendered. */
  ariaLabel?: string;
  className?: string;
}

/**
 * A single-choice select.
 *
 * Built on Radix rather than a styled <select> or a hand-rolled listbox: keyboard navigation,
 * typeahead, focus return, and the ARIA wiring are the parts that are invisible to the
 * developer who omits them and blocking to the user who needs them. This is the one category
 * of component D1 judged genuinely unsafe to hand-roll.
 */
export function Select({
  value,
  onValueChange,
  options,
  label,
  disabled,
  ariaLabel,
  className,
}: SelectProps) {
  const id = useId();

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      {label ? (
        <label htmlFor={id} className="text-sm font-medium text-text">
          {label}
        </label>
      ) : null}
      <RadixSelect.Root value={value} onValueChange={onValueChange} disabled={disabled}>
        <RadixSelect.Trigger
          id={id}
          aria-label={ariaLabel}
          className={cn(
            "inline-flex h-10 items-center justify-between gap-2 rounded border border-border",
            "bg-surface px-3 text-sm text-text disabled:opacity-50",
          )}
        >
          <RadixSelect.Value />
          <RadixSelect.Icon aria-hidden="true" className="text-text-muted">
            ▾
          </RadixSelect.Icon>
        </RadixSelect.Trigger>
        <RadixSelect.Portal>
          <RadixSelect.Content
            position="popper"
            sideOffset={4}
            className="z-50 overflow-hidden rounded border border-border bg-surface shadow-lg"
          >
            <RadixSelect.Viewport className="p-1">
              {options.map((option) => (
                <RadixSelect.Item
                  key={option.value}
                  value={option.value}
                  className={cn(
                    "flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm text-text",
                    "outline-none data-[highlighted]:bg-surface-raised",
                  )}
                >
                  <RadixSelect.ItemText>{option.label}</RadixSelect.ItemText>
                </RadixSelect.Item>
              ))}
            </RadixSelect.Viewport>
          </RadixSelect.Content>
        </RadixSelect.Portal>
      </RadixSelect.Root>
    </div>
  );
}
