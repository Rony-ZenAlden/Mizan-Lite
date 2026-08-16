import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Badge, Button, Checkbox, Input, Select, Switch, Table } from "@/shared/ui";

/**
 * Every interactive element has an accessible name (Step 10.3).
 *
 * # Why this is a test and not a review
 *
 * An element with no accessible name is announced as "button" by a screen reader, and there is no
 * way for the user to find out which. It is invisible to sighted review — the label is right there
 * on screen, drawn by an icon or a sibling element that the accessibility tree never sees.
 *
 * The rule this pins is narrow: a component in the shared library must not be *capable* of
 * rendering nameless. Where a name can only come from the caller, the component must refuse to
 * render without one rather than degrade quietly, and this checks the components that have their
 * own.
 */

describe("accessible names", () => {
  it("gives a button its label", () => {
    render(<Button>Save</Button>);
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("binds an input to its label rather than leaving them adjacent", () => {
    render(<Input label="Supplier name" value="" onChange={() => {}} />);

    // getByLabelText resolves through the accessibility tree, so this passes only if the label is
    // BOUND — a label sitting next to an input looks identical on screen and is nameless to a
    // screen reader.
    expect(screen.getByLabelText("Supplier name")).toBeInTheDocument();
  });

  it("names a select from its label", () => {
    render(
      <Select
        label="Payment method"
        value="cash"
        onValueChange={() => {}}
        options={[{ value: "cash", label: "Cash" }]}
      />,
    );
    expect(screen.getByRole("combobox", { name: /Payment method/ })).toBeInTheDocument();
  });

  it("names a checkbox and a switch", () => {
    render(
      <>
        <Checkbox label="Is a customer" checked={false} onCheckedChange={() => {}} />
        <Switch label="Tax exempt" checked={false} onCheckedChange={() => {}} />
      </>,
    );
    expect(screen.getByRole("checkbox", { name: "Is a customer" })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Tax exempt" })).toBeInTheDocument();
  });

  it("gives a table a caption, so its rows have a context when read aloud", () => {
    render(
      <Table<{ id: string; name: string }>
        caption="Suppliers"
        rowKey={(row) => row.id}
        rows={[{ id: "1", name: "Acme" }]}
        columns={[{ key: "name", header: "Name", cell: (row) => row.name }]}
      />,
    );

    // A table with no caption is a grid of numbers with no announced purpose. Forty rows into it,
    // a screen-reader user has no way to know what they are reading.
    expect(screen.getByRole("table", { name: "Suppliers" })).toBeInTheDocument();
  });

  it("does not convey meaning by colour alone", () => {
    render(
      <>
        <Badge tone="success">Invoiced</Badge>
        <Badge tone="danger">Out of balance</Badge>
      </>,
    );

    // A tone is a colour. The TEXT carries the meaning, and the test asserts the text is there —
    // because a badge that rendered an empty coloured pill would look meaningful and say nothing.
    expect(screen.getByText("Invoiced")).toBeInTheDocument();
    expect(screen.getByText("Out of balance")).toBeInTheDocument();
  });
});
