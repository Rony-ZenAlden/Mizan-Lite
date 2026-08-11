import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { BOTH_DIRECTIONS, renderIn } from "@/test/render";
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Dialog,
  EmptyState,
  Input,
  Select,
  Skeleton,
  Spinner,
  Switch,
  Table,
  Tabs,
} from "./index";

// Every primitive is rendered in BOTH directions. A component that only works in one is a
// component that will be rewritten later (§22.3), and the physical-class lint rule catches the
// class names but not, say, a hardcoded transform.

describe.each(BOTH_DIRECTIONS)("primitives render in $dir", ({ locale, dir }) => {
  it("sets the document direction", () => {
    renderIn(<Button>ok</Button>, { locale });
    expect(document.documentElement.dir).toBe(dir);
  });

  it("Button", () => {
    renderIn(<Button variant="primary">Save</Button>, { locale });
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("Input binds its label", () => {
    renderIn(<Input label="Code" />, { locale });
    // getByLabelText only succeeds if the label/id wiring is correct, which is the point.
    expect(screen.getByLabelText("Code")).toBeInTheDocument();
  });

  it("Input exposes its error to assistive technology", () => {
    renderIn(<Input label="Code" error="Required" />, { locale });
    const field = screen.getByLabelText("Code");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription("Required");
  });

  it("Checkbox and Switch bind their labels", () => {
    renderIn(
      <>
        <Checkbox checked={false} onCheckedChange={() => {}} label="Active" />
        <Switch checked onCheckedChange={() => {}} label="Enabled" />
      </>,
      { locale },
    );
    expect(screen.getByRole("checkbox", { name: "Active" })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Enabled" })).toBeChecked();
  });

  it("Table renders headers and rows", () => {
    renderIn(
      <Table
        columns={[
          { key: "a", header: "Code", cell: (r: { code: string }) => r.code },
          { key: "b", header: "Qty", cell: () => 1, numeric: true },
        ]}
        rows={[{ code: "SYP" }]}
        rowKey={(r) => r.code}
      />,
      { locale },
    );
    expect(screen.getByRole("columnheader", { name: "Code" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "SYP" })).toBeInTheDocument();
  });

  it("Feedback primitives", () => {
    renderIn(
      <>
        <Spinner />
        <Skeleton className="h-4 w-4" />
        <EmptyState title="Nothing here" />
        <Alert tone="danger" title="Problem" />
      </>,
      { locale },
    );
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    // A danger alert is assertive, so a screen reader announces it rather than queueing it.
    expect(screen.getByRole("alert")).toHaveTextContent("Problem");
  });
});

describe("EmptyState tones", () => {
  it("has a permission-denied variant awaiting its Phase 1 producer", () => {
    renderIn(<EmptyState tone="denied" title="Not allowed" description="Ask an administrator" />);
    expect(screen.getByText("Not allowed")).toBeInTheDocument();
    expect(screen.getByText("Ask an administrator")).toBeInTheDocument();
  });
});

// ── the accessibility properties that are actually hard ─────────────────────────

describe("Dialog", () => {
  function Harness() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <Button onClick={() => setOpen(true)}>Open</Button>
        <Dialog open={open} onOpenChange={setOpen} title="Confirm" description="Are you sure?">
          <Input label="Reason" />
        </Dialog>
      </>
    );
  }

  it("moves focus into the dialog and restores it on close", async () => {
    const user = userEvent.setup();
    renderIn(<Harness />);

    const trigger = screen.getByRole("button", { name: "Open" });
    trigger.focus();
    await user.click(trigger);

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveAccessibleName("Confirm");
    // Focus must be inside the dialog, or a keyboard user is stranded behind the overlay.
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    // Focus returning to the trigger is what makes Escape non-destructive for keyboard users.
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });
});

describe("Select", () => {
  it("is operable by keyboard alone", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();

    renderIn(
      <Select
        value="en"
        onValueChange={onValueChange}
        ariaLabel="Language"
        options={[
          { value: "en", label: "English" },
          { value: "ar", label: "العربية" },
        ]}
      />,
    );

    const trigger = screen.getByRole("combobox", { name: "Language" });
    trigger.focus();
    await user.keyboard("{Enter}");

    await screen.findByRole("listbox");
    await user.keyboard("{ArrowDown}{Enter}");

    await waitFor(() => expect(onValueChange).toHaveBeenCalledWith("ar"));
  });
});

describe("Tabs", () => {
  it("switches panels with the arrow keys", async () => {
    const user = userEvent.setup();

    function Harness() {
      const [value, setValue] = useState("one");
      return (
        <Tabs
          value={value}
          onValueChange={setValue}
          items={[
            { value: "one", label: "One", content: <p>First panel</p> },
            { value: "two", label: "Two", content: <p>Second panel</p> },
          ]}
        />
      );
    }
    renderIn(<Harness />);

    // Focused via user-event rather than a bare .focus(), so the roving-focus state update
    // happens inside act() instead of warning.
    await user.click(screen.getByRole("tab", { name: "One" }));
    await user.keyboard("{ArrowRight}");

    await waitFor(() => expect(screen.getByText("Second panel")).toBeInTheDocument());
  });
});

describe("Button", () => {
  it("marks itself busy and blocks interaction while loading", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    renderIn(
      <Button loading onClick={onClick}>
        Save
      </Button>,
    );

    const button = screen.getByRole("button", { name: /Save/ });
    expect(button).toHaveAttribute("aria-busy", "true");
    await user.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("Badge", () => {
  it("renders its children", () => {
    renderIn(<Badge>Customer</Badge>);
    expect(screen.getByText("Customer")).toBeInTheDocument();
  });

  it("carries no role and no live region", () => {
    // A badge repeats what the row already says. Announcing "customer" twice helps nobody, and
    // a status role here would interrupt a screen reader on every list render.
    renderIn(<Badge tone="info">Supplier</Badge>);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("styles every tone from tokens rather than hardcoded colours", () => {
    const tones = ["neutral", "info", "success", "warning", "danger"] as const;
    for (const tone of tones) {
      const { container, unmount } = renderIn(<Badge tone={tone}>x</Badge>);
      const className = container.firstElementChild?.className ?? "";
      // No hex, no rgb: the token rule Step 0.11 D6 set for every primitive.
      expect(className).not.toMatch(/#[0-9a-f]{3,6}|rgb\(/i);
      expect(className).not.toBe("");
      unmount();
    }
  });
});
