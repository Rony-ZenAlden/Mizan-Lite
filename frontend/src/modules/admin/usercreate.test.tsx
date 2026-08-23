import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { UsersScreen } from "@/modules/admin/UsersScreen";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    users: vi.fn(),
    roles: vi.fn(),
    createUser: vi.fn(),
    setUserActive: vi.fn(),
  };
});

const ADMIN_ROLE = {
  id: "r1", code: "administrator", name: "Administrator",
  description: "", isSystem: true, userCount: 1, isActive: true,
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/",
  });
  vi.mocked(wails.roles).mockResolvedValue([ADMIN_ROLE]);
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [wails.PERMISSIONS.userView, wails.PERMISSIONS.userManage],
  });
});

/**
 * Adding a user, driven the way a person drives it.
 *
 * # Why this exists
 *
 * A report that "adding a user fails or does not persist" could not be reproduced: the binding
 * path creates, lists, assigns a role and signs in, asserted by a Go test. So the question was
 * whether the SCREEN loses it — a mutation that does not refresh the list looks exactly like a
 * user that was not saved.
 *
 * It does not, and this is the guard that keeps it that way. The failure it would catch is the
 * one that reads as data loss and is not: a created user, absent from the table, retyped by an
 * administrator who then finds two.
 */
describe("adding a user", () => {
  it("sends what was typed and shows the user in the list afterwards", async () => {
    const created = {
      id: "u2", username: "sara", displayName: "Sara", email: "",
      isActive: true, isSystem: false, roles: [] as string[],
    };

    // Empty first, then holding the new user — which is what the refetch after a successful
    // creation must produce. A mutation that does not invalidate leaves the first answer on
    // screen forever.
    vi.mocked(wails.users)
      .mockResolvedValueOnce([])
      .mockResolvedValue([created]);
    vi.mocked(wails.createUser).mockResolvedValue(created);

    renderApp(<UsersScreen />);
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: /Add user/i }));

    await user.type(await screen.findByLabelText(/Username/i), "sara");
    await user.type(screen.getByLabelText("Name"), "Sara");
    await user.type(screen.getByLabelText(/Password/i), "a sufficiently long passphrase");

    await user.click(screen.getByRole("button", { name: /^Create/i }));

    await waitFor(() => {
      expect(wails.createUser).toHaveBeenCalledTimes(1);
    });

    // What was typed reached the binding — not a stale closure, not an empty object.
    const sent = vi.mocked(wails.createUser).mock.calls[0]?.[0];
    expect(sent?.username).toBe("sara");
    expect(sent?.password).toBe("a sufficiently long passphrase");

    // And the list refreshed. This is the assertion that separates "it failed" from "it worked
    // and the screen did not say so" — the two are indistinguishable to the person using it.
    expect(await screen.findByText("sara")).toBeInTheDocument();
  });

  it("shows the reason when the backend refuses, rather than closing silently", async () => {
    vi.mocked(wails.users).mockResolvedValue([]);
    vi.mocked(wails.createUser).mockRejectedValue(
      Object.assign(new Error("too short"), {
        code: "identity.password_too_short",
        messageKey: "identity.password_too_short",
        params: { min: "12" },
      }),
    );

    renderApp(<UsersScreen />);
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: /Add user/i }));
    await user.type(await screen.findByLabelText(/Username/i), "sara");
    await user.type(screen.getByLabelText(/Password/i), "short");
    await user.click(screen.getByRole("button", { name: /^Create/i }));

    // A dialog that closed on a refusal would look exactly like a save that did not persist.
    expect(await screen.findByText(/could not be/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Username/i)).toBeInTheDocument();
  });
});
