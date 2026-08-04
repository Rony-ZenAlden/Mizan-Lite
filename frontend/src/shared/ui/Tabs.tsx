import * as RadixTabs from "@radix-ui/react-tabs";
import type { ReactNode } from "react";
import { cn } from "./cn";

export interface TabItem {
  value: string;
  label: string;
  content: ReactNode;
}

export function Tabs({
  value,
  onValueChange,
  items,
  className,
}: {
  value: string;
  onValueChange: (value: string) => void;
  items: TabItem[];
  className?: string;
}) {
  return (
    <RadixTabs.Root value={value} onValueChange={onValueChange} className={cn("flex flex-col", className)}>
      <RadixTabs.List className="flex gap-1 border-b border-border">
        {items.map((item) => (
          <RadixTabs.Trigger
            key={item.value}
            value={item.value}
            className={cn(
              "-mb-px border-b-2 border-transparent px-3 py-2 text-sm text-text-muted",
              "data-[state=active]:border-primary data-[state=active]:text-text",
            )}
          >
            {item.label}
          </RadixTabs.Trigger>
        ))}
      </RadixTabs.List>
      {items.map((item) => (
        <RadixTabs.Content key={item.value} value={item.value} className="pt-4">
          {item.content}
        </RadixTabs.Content>
      ))}
    </RadixTabs.Root>
  );
}
