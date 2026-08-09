import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "@/app/shell/AppShell";
import { useSessionStore } from "@/app/session/session";
import { AuditScreen } from "@/modules/admin/AuditScreen";
import { SessionsScreen } from "@/modules/admin/SessionsScreen";
import { UsersScreen } from "@/modules/admin/UsersScreen";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    users: vi.fn(),
    setUserActive: vi.fn(),
    createUser: vi.fn(),
    roles: vi.fn(),
    adminSessions: vi.fn(),
    revokeSession: vi.fn(),
    auditEntries: vi.fn(),
    health: vi.fn(),
  };
});

const ALICE: wails.User = {
  id: "u-1", username: "alice", displayName: "Alice", email: "",
  isActive: true, isSystem: true,
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.users).mockResolvedValue([ALICE]);
  vi.mocked(wails.roles).mockResolvedValue([]);
  vi.mocked(wails.health).mockResolvedValue({
    version: "test", commit: "", buildTime: "", goVersion: "", platform: "test",
  });
  useSessionStore.getState().clear();
});

afterEach(() => vi.clearAllMocks());

function signedInWith(permissions: string[]) {
  useSessionStore.getState().setSession({ ...SIGNED_IN, permissions });
}

// ── permission-aware navigation (§FE.3) ─────────────────────────────────────────

describe("navigation", () => {
  it("shows only the screens the user may reach", async () => {
    signedInWith([wails.PERMISSIONS.userView]);
    renderApp(<AppShell />);

    const nav = await screen.findByRole("navigation");
    expect(within(nav).getByText("Users")).toBeInTheDocument();
    // Cosmetic, and that is the point: it stops a cashier learning to ignore errors by
    // clicking a menu item that always refuses. It protects nothing.
    expect(within(nav).queryByText("Roles")).not.toBeInTheDocument();
    expect(within(nav).queryByText("Sessions")).not.toBeInTheDocument();
  });

  it("always offers the user their own password", async () => {
    // The person who most needs it may hold no permission at all: must_change is typically set
    // on an account created seconds earlier with no role (1.11 D3).
    signedInWith([]);
    renderApp(<AppShell />);

    const nav = await screen.findByRole("navigation");
    expect(within(nav).getByText("My password")).toBeInTheDocument();
  });

  it("explains a screen reached without the permission", async () => {
    signedInWith([]);
    renderApp(<AppShell />, { route: "/admin/users" });

    // The EmptyState `denied` variant built in 0.11 D6 finally has a producer. A blank page or
    // a silent redirect would leave the user unable to say what to ask for.
    expect(await screen.findByText(/do not have access/i)).toBeInTheDocument();
    expect(screen.getByText(/identity\.user\.view/)).toBeInTheDocument();
  });
});

// ── users ───────────────────────────────────────────────────────────────────────

describe("UsersScreen", () => {
  it("renders a refusal from the backend rather than hiding the button", async () => {
    // The three bricking rules live in the SERVICE (1.11 D6). The screen offers the action and
    // shows why it was refused — a control that explains itself teaches the rule; a missing one
    // teaches nothing.
    signedInWith([wails.PERMISSIONS.userView, wails.PERMISSIONS.userManage]);
    vi.mocked(wails.setUserActive).mockRejectedValue(
      new wails.BindingError({
        code: "identity.self_deactivation",
        messageKey: "identity.self_deactivation",
      }),
    );

    const { user } = renderApp(<UsersScreen />);
    await user.click(await screen.findByRole("button", { name: /deactivate/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/cannot deactivate the account/i);
  });

  it("hides management actions from someone who may only look", async () => {
    signedInWith([wails.PERMISSIONS.userView]);
    renderApp(<UsersScreen />);

    expect(await screen.findByText("alice")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /deactivate/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /add user/i })).not.toBeInTheDocument();
  });
});

// ── the audit viewer's three states (D7) ────────────────────────────────────────

describe("AuditScreen", () => {
  it("tells withheld apart from empty apart from present", async () => {
    // The whole point of 1.6: a blank cell cannot be told from a forbidden one, so nobody ever
    // asks for the permission they are missing. The DTO omits the KEY rather than blanking it,
    // and the viewer must render that difference.
    signedInWith([wails.PERMISSIONS.auditView]);
    vi.mocked(wails.auditEntries).mockResolvedValue([
      {
        id: "1", occurredAt: "2026-01-01", actorUserId: "u", actorName: "Alice",
        correlationId: "c", action: "identity.user.created", entityType: "identity.user",
        entityId: "u", entityLabel: "karim", source: "ui",
        // Present, with content.
        beforeJson: "", afterJson: `{"username":"karim"}`, changedFields: "",
      },
      {
        id: "2", occurredAt: "2026-01-01", actorUserId: "u", actorName: "Alice",
        correlationId: "c", action: "identity.user.password_changed", entityType: "identity.user",
        entityId: "u", entityLabel: "karim", source: "ui",
        // Present, and genuinely empty: a password change carries no payload by design (1.7).
        beforeJson: "", afterJson: "", changedFields: `["password"]`,
      },
      {
        id: "3", occurredAt: "2026-01-01", actorUserId: "u", actorName: "Alice",
        correlationId: "c", action: "identity.user.deactivated", entityType: "identity.user",
        entityId: "u", entityLabel: "karim", source: "ui",
        // ABSENT: the caller lacks audit.entry.view_payload.
      },
    ]);

    renderApp(<AuditScreen />);

    expect(await screen.findByText(/"username":"karim"/)).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.getByText(/withheld/i)).toBeInTheDocument();
  });

  it("names the system when nobody was acting", async () => {
    signedInWith([wails.PERMISSIONS.auditView]);
    vi.mocked(wails.auditEntries).mockResolvedValue([
      {
        id: "1", occurredAt: "2026-01-01", actorUserId: "", actorName: "",
        correlationId: "c", action: "setup.completed", entityType: "setup.installation",
        entityId: "c", entityLabel: "Demo", source: "system",
      },
    ]);

    renderApp(<AuditScreen />);
    expect(await screen.findByText("System")).toBeInTheDocument();
  });
});

// ── sessions ────────────────────────────────────────────────────────────────────

describe("SessionsScreen", () => {
  it("marks the caller's own session rather than hiding it", async () => {
    // "Sign me out everywhere" is a legitimate thing to want. Ending your own is permitted, and
    // marked, so it is a decision rather than a surprise.
    signedInWith([wails.PERMISSIONS.sessionView, wails.PERMISSIONS.sessionRevoke]);
    vi.mocked(wails.adminSessions).mockResolvedValue([
      {
        id: "s-1", userId: "u-1", username: "alice", displayName: "Alice",
        startedAt: "", lastSeen: "", expiresAt: "", deviceInfo: "counter", current: true,
      },
      {
        id: "s-2", userId: "u-2", username: "karim", displayName: "Karim",
        startedAt: "", lastSeen: "", expiresAt: "", deviceInfo: "office", current: false,
      },
    ]);
    vi.mocked(wails.revokeSession).mockResolvedValue(true);

    const { user } = renderApp(<SessionsScreen />);

    expect(await screen.findByText(/Alice \(this device\)/)).toBeInTheDocument();
    expect(screen.getByText("Karim")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign out this device/i })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^sign out$/i }));
    await waitFor(() => expect(wails.revokeSession).toHaveBeenCalledWith("s-2"));
  });
});
