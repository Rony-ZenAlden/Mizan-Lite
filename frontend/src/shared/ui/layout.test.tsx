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

describe("the layout layer is actually used", () => {
  it("leaves no screen hand-rolling a page header", async () => {
    // # Why a source scan and not a review
    //
    // The 10.12 audit found the same four lines — a flex column, an h2 at text-base, a p at
    // text-sm — repeated across 21 files. Nothing was wrong with any one of them; together they
    // were the reason the product read as a database viewer, because a change to the type scale
    // meant twenty-one edits and therefore never happened.
    //
    // `PageHeader` is now that shape with a name. This stops the next screen reintroducing the
    // old one, which is how the count got to 21 in the first place.
    const { readdirSync, readFileSync, statSync } = await import("node:fs");
    const { join, relative, resolve } = await import("node:path");

    const root = resolve(process.cwd(), "src/modules");
    const files: string[] = [];
    const walk = (dir: string) => {
      for (const entry of readdirSync(dir)) {
        const path = join(dir, entry);
        if (statSync(path).isDirectory()) walk(path);
        else if (/\.tsx$/.test(entry) && !/\.test\./.test(entry)) files.push(path);
      }
    };
    walk(root);
    expect(files.length).toBeGreaterThan(20);

    // The exact heading style PageHeader replaced. A screen that wants a different heading is
    // free to write one; what it may not do is re-create this one.
    const handRolled = /<h2 className="text-base font-medium text-text">/;

    // Two files keep their own heading, for reasons that are about what they ARE:
    //
    //   help/HelpScreen — the guide renders its own title in whichever language the READER
    //     chose, which may differ from the application's. A PageHeader would translate it with
    //     the app's locale and undo that.
    //   sales/ShiftBar — a strip above the till, not a page. A page header inside another
    //     screen's layout is a heading claiming to be a screen.
    const exempt = new Set(["help/HelpScreen.tsx", "sales/ShiftBar.tsx"]);

    const offenders = files
      .filter((file) => handRolled.test(readFileSync(file, "utf8")))
      .map((file) => relative(root, file))
      .filter((file) => !exempt.has(file.replace(/\\/g, "/")));

    expect(offenders).toEqual([]);
  });
});
