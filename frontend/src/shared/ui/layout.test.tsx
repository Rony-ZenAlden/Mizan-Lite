import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Card, PageHeader, StatTile, StepList, ToolBar } from "@/shared/ui";

/**
 * The layout layer (Step 10.13).
 *
 * The 10.12 audit counted the problem: 24 of 30 screens were a heading, a filter, and one table,
 * so nothing on screen distinguished a three-field form from a forty-row ledger. These components
 * are that vocabulary, and these tests pin the parts that carry meaning rather than styling.
 */
describe("layout", () => {
  it("gives a page header a heading and its actions", () => {
    render(
      <PageHeader title="Deliveries" description="What arrived" actions={<button>New</button>} />,
    );
    expect(screen.getByRole("heading", { name: "Deliveries" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New" })).toBeInTheDocument();
  });

  it("makes a card a landmark with its own heading", () => {
    render(<Card title="Charges">rows</Card>);
    // A card without a heading is a box. The heading is what says "these things belong together",
    // which is most of what a reader needs before reading anything.
    expect(screen.getByRole("heading", { name: "Charges" })).toBeInTheDocument();
  });

  it("requires a stat tile to say what it is measuring", () => {
    render(<StatTile label="Revenue" value="4,500.00" caption="This month · from sales" />);

    // 8.7 D4: a number without its qualification is a number that will be misread. "Stock value
    // 1,200.00" beside "revenue 450.00" reads as this month's purchases unless something says
    // otherwise — so the caption is a required prop, and this asserts it renders.
    expect(screen.getByText("Revenue")).toBeInTheDocument();
    expect(screen.getByText("4,500.00")).toBeInTheDocument();
    expect(screen.getByText("This month · from sales")).toBeInTheDocument();
  });

  it("marks the current step and the ones already done", () => {
    render(
      <StepList
        current="count"
        steps={[
          { id: "order", label: "Choose the order", done: true },
          { id: "count", label: "Say what arrived" },
        ]}
      />,
    );

    const current = screen.getByRole("button", { name: /Say what arrived/ });
    expect(current).toHaveAttribute("aria-current", "step");

    // A completed step shows a tick rather than its number, so a glance says how far in you are.
    expect(screen.getByText("✓")).toBeInTheDocument();
  });

  it("keeps toolbar filters and actions apart", () => {
    render(
      <ToolBar actions={<button>Export</button>}>
        <input aria-label="Search" />
      </ToolBar>,
    );
    expect(screen.getByLabelText("Search")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Export" })).toBeInTheDocument();
  });
});
