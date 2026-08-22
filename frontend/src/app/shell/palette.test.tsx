import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { CommandPalette } from "@/app/shell/CommandPalette";
import { availableActions, destinationFor, knownKinds, QUICK_ACTIONS } from "@/app/shell/commands";
import { ROUTES } from "@/app/shell/routes";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return { ...actual, preferences: vi.fn(), globalSearch: vi.fn() };
});

const EVERYTHING = Object.values(wails.PERMISSIONS);

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en",
    theme: "system",
    availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.globalSearch).mockResolvedValue({ query: "", results: [], failed: [] });
  useSessionStore.getState().setSession({ ...SIGNED_IN, permissions: EVERYTHING });
});

afterEach(() => {
  vi.clearAllMocks();
  useSessionStore.getState().clear();
});

// ── the declaration, checked without rendering anything ─────────────────────────

describe("the command list", () => {
  /*
   * The 10.12 finding, in its sixth form: built, tested, never connected. An action pointing at
   * a path no route serves lands the user on an empty screen, and nothing in a rendering test
   * would notice — the button appears, the click works, the destination is blank.
   */
  it("points every quick action at a route that exists", () => {
    const paths = new Set(ROUTES.map((route) => route.path));
    for (const action of QUICK_ACTIONS) {
      expect(paths, `the "${action.key}" action goes to ${action.to}, which no route serves`)
        .toContain(action.to);
    }
  });

  it("gates every quick action on a permission the backend declares", () => {
    // An action gated on an invented permission is either always hidden or always shown, and
    // nobody can tell which. Three invented permissions got as far as a shipped screen in 7.6.
    const declared = new Set<string>(Object.values(wails.PERMISSIONS));
    for (const action of QUICK_ACTIONS) {
      if (!action.permission) continue;
      expect(declared, `the "${action.key}" action requires "${action.permission}"`)
        .toContain(action.permission);
    }
  });

  it("sends every record kind it claims to know somewhere a route serves", () => {
    const paths = new Set(ROUTES.map((route) => route.path));
    for (const kind of knownKinds()) {
      const to = destinationFor(kind);
      expect(to, `${kind} has no destination`).toBeTruthy();
      expect(paths, `${kind} opens ${to}, which no route serves`).toContain(to as string);
    }
  });

  it("hides the actions a user may not run", () => {
    // Cosmetic, like the sidebar (§FE.3): it protects nothing, and the reason to do it is that
    // a shortcut which always refuses teaches people to ignore errors.
    expect(availableActions([])).toEqual([]);
    expect(availableActions(EVERYTHING).length).toBe(QUICK_ACTIONS.length);

    const onlyTill = availableActions([wails.PERMISSIONS.saleDraft]);
    expect(onlyTill.map((action) => action.key)).toEqual(["newSale"]);
  });
});

// ── the palette itself ──────────────────────────────────────────────────────────

describe("the command palette", () => {
  it("offers actions and screens before anything is typed", async () => {
    renderApp(<CommandPalette open onOpenChange={() => {}} />);

    // The verbs, which is the point: a palette that only lists screens makes a user translate
    // "sell something" into "the till" before it can help them.
    expect(await screen.findByRole("option", { name: /New sale/i })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /Backups/i })).toBeInTheDocument();
  });

  it("narrows to what was typed", async () => {
    const { user } = renderApp(<CommandPalette open onOpenChange={() => {}} />);

    await user.type(screen.getByRole("searchbox"), "supplier pay");

    await waitFor(() => {
      expect(screen.queryByRole("option", { name: /New sale/i })).not.toBeInTheDocument();
    });
    expect(screen.getByRole("option", { name: /Supplier payments/i })).toBeInTheDocument();
  });

  it("navigates on Enter, and closes as it goes", async () => {
    const onOpenChange = vi.fn();
    const { user } = renderApp(<CommandPalette open onOpenChange={onOpenChange} />);

    await user.type(screen.getByRole("searchbox"), "New sale");
    await waitFor(() => expect(screen.getByRole("option", { name: /New sale/i })).toBeInTheDocument());
    await user.keyboard("{Enter}");

    // Closing MATTERS. A palette left open over the screen it just opened hides the thing the
    // user asked for, and the next keystroke goes into the search box.
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("asks the backend for records and offers what comes back", async () => {
    vi.mocked(wails.globalSearch).mockResolvedValue({
      query: "cem",
      results: [
        { kind: "catalog.product", id: "p1", label: "Bag of cement", subtitle: "CEMENT" },
        // A kind the palette has no destination for must be DROPPED, not guessed at.
        { kind: "astrology.horoscope", id: "x1", label: "Cement rising", subtitle: "" },
      ],
      failed: [],
    });

    const { user } = renderApp(<CommandPalette open onOpenChange={() => {}} />);
    await user.type(screen.getByRole("searchbox"), "cem");

    expect(await screen.findByRole("option", { name: /Bag of cement/i })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /Cement rising/i })).not.toBeInTheDocument();
  });

  it("says search is degraded rather than showing nothing found", async () => {
    /*
     * "Nothing matches" and "the sales index is broken" are different answers, and the backend
     * distinguishes them precisely so a screen does not have to guess. Collapsing them would
     * send somebody looking for a record that is there.
     */
    vi.mocked(wails.globalSearch).mockResolvedValue({
      query: "cem",
      results: [],
      failed: ["sales"],
    });

    const { user } = renderApp(<CommandPalette open onOpenChange={() => {}} />);
    await user.type(screen.getByRole("searchbox"), "cem");

    expect(await screen.findByText(/sales/)).toBeInTheDocument();
  });

  it("does not ask the backend for one character", async () => {
    // Two is the backend's minimum, so a one-character query spends a round trip to be refused.
    const { user } = renderApp(<CommandPalette open onOpenChange={() => {}} />);
    await user.type(screen.getByRole("searchbox"), "c");

    await waitFor(() => expect(screen.getByRole("searchbox")).toHaveValue("c"));
    expect(wails.globalSearch).not.toHaveBeenCalled();
  });

  it("hides screens the signed-in user may not open", async () => {
    useSessionStore.getState().setSession({ ...SIGNED_IN, permissions: [] });
    renderApp(<CommandPalette open onOpenChange={() => {}} />);

    // The guide has no permission, so it stays. Backups requires one, so it goes.
    expect(await screen.findByRole("option", { name: /How to use Mizan/i })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /Backups/i })).not.toBeInTheDocument();
  });
});
