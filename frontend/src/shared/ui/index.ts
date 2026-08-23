// The primitive layer (Step 0.11 D6).
//
// Every primitive is styled from tokens, uses logical properties only, and has a
// keyboard-visible focus ring inherited from the global rule. Nothing here hardcodes a colour,
// radius, shadow, or type size.

export { cn } from "./cn";
export { Button, type ButtonProps, type ButtonVariant, type ButtonSize } from "./Button";
export { Input, type InputProps } from "./Input";
export { Select, type SelectOption, type SelectProps } from "./Select";
export { Checkbox, Switch, type CheckboxProps, type SwitchProps } from "./Toggles";
export { ChoiceGroup, type Choice } from "./Choice";
export { Dialog, Sheet, DialogClose } from "./Dialog";
export { Tabs, type TabItem } from "./Tabs";
export { Table, type Column, type TableProps } from "./Table";
export { Badge, type BadgeTone } from "./Badge";
export { Tooltip, TooltipProvider } from "./Tooltip";
export { ToastProvider, useToast, type ToastTone } from "./Toast";
export {
  Alert,
  EmptyState,
  Skeleton,
  Spinner,
  type AlertTone,
  type EmptyStateTone,
} from "./Feedback";

// The layout layer (10.13): the vocabulary that distinguishes a form from a ledger.
export { PageHeader, Card, StatTile, ToolBar, StepList } from "./Layout";
export { ExportMenu } from "./ExportMenu";
