import * as RadixCheckbox from "@radix-ui/react-checkbox";
import * as RadixSwitch from "@radix-ui/react-switch";
import { useId } from "react";
import { cn } from "./cn";

// Checkbox and Switch share a file because they share a shape: a labelled boolean control.
// They are NOT the same component — a checkbox is part of a form that gets submitted, a switch
// applies immediately — and conflating them is how a settings screen ends up with a Save button
// nobody expected.

export interface CheckboxProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  disabled?: boolean;
}

export function Checkbox({ checked, onCheckedChange, label, disabled }: CheckboxProps) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <RadixCheckbox.Root
        id={id}
        checked={checked}
        onCheckedChange={(next) => onCheckedChange(next === true)}
        disabled={disabled}
        className={cn(
          "flex h-4 w-4 items-center justify-center rounded-sm border border-border-strong",
          "bg-surface data-[state=checked]:border-primary data-[state=checked]:bg-primary",
          "disabled:opacity-50",
        )}
      >
        <RadixCheckbox.Indicator className="text-xs text-primary-fg">✓</RadixCheckbox.Indicator>
      </RadixCheckbox.Root>
      <label htmlFor={id} className="text-sm text-text">
        {label}
      </label>
    </div>
  );
}

export interface SwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  disabled?: boolean;
}

export function Switch({ checked, onCheckedChange, label, disabled }: SwitchProps) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <RadixSwitch.Root
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        className={cn(
          "relative h-5 w-9 rounded-full border border-border-strong bg-surface-sunken transition-colors",
          "data-[state=checked]:border-primary data-[state=checked]:bg-primary disabled:opacity-50",
        )}
      >
        {/* Logical inset properties, so the thumb travels the correct way in RTL. */}
        <RadixSwitch.Thumb
          className={cn(
            "block h-3.5 w-3.5 rounded-full bg-surface shadow-sm transition-transform",
            "translate-x-0.5 rtl:-translate-x-0.5",
            "data-[state=checked]:translate-x-4 rtl:data-[state=checked]:-translate-x-4",
          )}
        />
      </RadixSwitch.Root>
      <label htmlFor={id} className="text-sm text-text">
        {label}
      </label>
    </div>
  );
}
