import { useId, type InputHTMLAttributes } from "react";

/** A labelled checkbox. The label is passed in already translated. */
export function Checkbox({ label, ...input }: { label: string } & Omit<InputHTMLAttributes<HTMLInputElement>, "type">) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <input id={id} type="checkbox" className="h-4 w-4 accent-primary" {...input} />
      <label htmlFor={id} className="text-sm">
        {label}
      </label>
    </div>
  );
}
