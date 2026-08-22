import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { HelpScreen } from "@/modules/help/HelpScreen";
import { CHAPTERS, READING_ORDER, type Block } from "@/modules/help/content";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return { ...actual, preferences: vi.fn() };
});

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  useSessionStore.getState().setSession({ ...SIGNED_IN, permissions: [] });
});

// ── the content gate ────────────────────────────────────────────────────────────

/** Every string pair a block carries, so nothing can be checked in one language only. */
function pairs(block: Block): Array<[string, string, string]> {
  switch (block.kind) {
    case "text":
    case "note":
      return [[block.kind, block.en, block.ar]];
    case "steps":
      return block.en.map((en, i) => [`steps[${i}]`, en, block.ar[i] ?? ""]);
    case "example":
      return [
        ["example.title", block.titleEn, block.titleAr],
        ...block.rows.flatMap((row, i): Array<[string, string, string]> => [
          [`example.rows[${i}].do`, row.doEn, row.doAr],
          [`example.rows[${i}].then`, row.thenEn, row.thenAr],
        ]),
      ];
    case "flow":
      return block.stages.flatMap((stage, i): Array<[string, string, string]> => [
        [`flow.stages[${i}]`, stage.en, stage.ar],
        [`flow.stages[${i}].module`, stage.moduleEn, stage.moduleAr],
      ]);
  }
}

describe("guide content", () => {
  it("says everything in both languages", () => {
    // The gate the whole content design exists for. Prose split across a flat translation file
    // drifts silently: a paragraph is rewritten in English and the Arabic still says the old
    // thing, and nothing is wrong enough to notice. Side by side, a missing half is a test
    // failure.
    const missing: string[] = [];

    for (const chapter of CHAPTERS) {
      if (!chapter.titleEn.trim() || !chapter.titleAr.trim()) {
        missing.push(`chapter ${chapter.id}: title`);
      }
      for (const section of chapter.sections) {
        for (const [field, en, ar] of [
          ["title", section.titleEn, section.titleAr],
          ["lead", section.leadEn, section.leadAr],
        ] as const) {
          if (!en.trim() || !ar.trim()) missing.push(`${section.id}: ${field}`);
        }
        section.blocks.forEach((block, index) => {
          for (const [what, en, ar] of pairs(block)) {
            if (!en.trim() || !ar.trim()) {
              missing.push(`${section.id}: block ${index} ${what}`);
            }
          }
        });
      }
    }

    expect(missing).toEqual([]);
  });

  it("carries enough to be a guide rather than a placeholder", () => {
    // A guard against the shape this test could otherwise pass with: an empty content file.
    expect(CHAPTERS.length).toBeGreaterThanOrEqual(4);
    expect(READING_ORDER.length).toBeGreaterThanOrEqual(8);

    const blocks = CHAPTERS.flatMap((c) => c.sections).flatMap((s) => s.blocks);
    expect(blocks.length).toBeGreaterThanOrEqual(20);

    // Every kind is actually used. A renderer branch nothing produces is a branch nobody has
    // seen — and the flow and example blocks are the ones that carry the interactive content.
    const kinds = new Set(blocks.map((b) => b.kind));
    for (const kind of ["text", "steps", "note", "example", "flow"]) {
      expect(kinds).toContain(kind);
    }
  });

  it("has no duplicate section ids", () => {
    // The id is what the contents list and the previous/next navigation both key on. A duplicate
    // makes two entries open the same page, and the second is unreachable.
    expect(new Set(READING_ORDER).size).toBe(READING_ORDER.length);
  });
});

// ── the screen ──────────────────────────────────────────────────────────────────

describe("guide screen", () => {
  it("opens in the application's language and can be switched without changing it", async () => {
    renderApp(<HelpScreen />, { locale: "en" });

    expect(await screen.findByText("How to Use Mizan ERP")).toBeInTheDocument();

    const { userEvent } = await import("@testing-library/user-event").then((m) => ({
      userEvent: m.default,
    }));
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "العربية" }));

    expect(await screen.findByText("دليل استخدام نظام ميزان")).toBeInTheDocument();

    // The APPLICATION is still English. A guide that flipped the menu and the toolbar around the
    // reader would be worse than one in the wrong language — the person reading it is often not
    // the person the application is set up for.
    expect(document.documentElement.dir).toBe("ltr");
  });

  it("renders the guide's own direction on its own subtree", async () => {
    // The guide seeds from the APPLICATION's locale, which the provider loads from preferences —
    // not from the render helper's initial attribute. The first version of this test set only the
    // helper and got an English guide, which is the seeding working correctly.
    vi.mocked(wails.preferences).mockResolvedValue({
      locale: "ar", theme: "system", availableLocales: ["en", "ar"],
    });
    renderApp(<HelpScreen />, { locale: "ar" });

    const guide = await screen.findByText("دليل استخدام نظام ميزان");
    const section = guide.closest("section");
    expect(section).toHaveAttribute("dir", "rtl");
  });

  it("moves through the sections in reading order", async () => {
    renderApp(<HelpScreen />, { locale: "en" });

    const { userEvent } = await import("@testing-library/user-event").then((m) => ({
      userEvent: m.default,
    }));
    const user = userEvent.setup();

    // Previous is disabled at the start: a guide is read through the first time, and the position
    // indicator is what tells somebody how much is left.
    expect(await screen.findByRole("button", { name: /Previous/ })).toBeDisabled();
    expect(screen.getByText(`1 / ${READING_ORDER.length}`)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Next/ }));
    expect(await screen.findByText(`2 / ${READING_ORDER.length}`)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Previous/ })).toBeEnabled();
  });

  it("marks the open section in the contents, so a reader knows where they are", async () => {
    renderApp(<HelpScreen />, { locale: "en" });

    const contents = await screen.findByRole("navigation", { name: "Contents" });
    const current = within(contents)
      .getAllByRole("button")
      .filter((button) => button.getAttribute("aria-current") === "true");

    expect(current).toHaveLength(1);
  });

  it("is reachable by somebody who holds no permissions at all", async () => {
    // The session in this file's fixture has an empty permission list. The guide is where a user
    // who can do nothing else learns what they are looking at, so it is the one screen that must
    // not be gated.
    renderApp(<HelpScreen />, { locale: "en" });
    expect(await screen.findByText("How to Use Mizan ERP")).toBeInTheDocument();
  });
});

describe("coverage of the system", () => {
  it("walks through every module a user can reach", () => {
    // # Why this list is written out rather than derived
    //
    // A guide that covers "most of it" is one somebody stops trusting the first time they look up
    // the screen they are stuck on and find nothing. Deriving the list from ROUTES would keep it
    // in step automatically — and would also let a route be added with a guide section that says
    // nothing, because the id would match and the prose would be empty.
    //
    // So the list is deliberate, and adding a module means deciding what the guide says about it.
    const covered = new Set(READING_ORDER);

    for (const area of [
      "products",   // catalogue and registering what you sell
      "stock",      // stock levels, counting, adjustments
      "selling",    // the till, shifts, payment, returns
      "buying",     // orders, deliveries, bills, supplier payments
      "people",     // customers and suppliers
      "spending",   // expenses and debts
      "reports",    // statements, analysis, valuation
      "operations", // backups, restore, import, notices
      "settings",   // users, roles, sessions, audit
    ]) {
      expect(covered).toContain(area);
    }
  });

  it("gives each module enough steps to follow, not a sentence", () => {
    // A "step-by-step walkthrough" whose sections hold one paragraph is a summary. Each of these
    // sections must carry at least one ordered list, because that is what a person follows with
    // the screen open beside them.
    const walkthroughs = CHAPTERS.find((c) => c.id === "using");
    expect(walkthroughs).toBeDefined();

    for (const section of walkthroughs!.sections) {
      const steps = section.blocks.filter((b) => b.kind === "steps");
      expect(
        steps.length,
        `${section.id} has no ordered steps to follow`,
      ).toBeGreaterThanOrEqual(1);

      for (const block of steps) {
        if (block.kind !== "steps") continue;
        expect(
          block.en.length,
          `${section.id} has a step list too short to walk anybody through`,
        ).toBeGreaterThanOrEqual(3);
        // Both languages hold the SAME number of steps. A missing step in one language is a
        // reader following instructions that skip something.
        expect(block.ar.length, `${section.id} has a different step count in Arabic`).toBe(
          block.en.length,
        );
      }
    }
  });
});
