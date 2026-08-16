import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { NoticeCentre } from "@/modules/operations/NoticeCentre";
import { BackupScreen } from "@/modules/operations/BackupScreen";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    notices: vi.fn(),
    dismissNotice: vi.fn(),
    backups: vi.fn(),
    pendingRestore: vi.fn(),
    takeBackup: vi.fn(),
  };
});

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.backups).mockResolvedValue([]);
  vi.mocked(wails.pendingRestore).mockResolvedValue({
    from: "", replacingVersion: 0, restoringVersion: 0, safetyBackup: "", preparedAt: "",
  });
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [wails.PERMISSIONS.userManage, wails.PERMISSIONS.sessionView],
  });
});

describe("notice centre", () => {
  it("says when a check could not run, rather than showing nothing", async () => {
    vi.mocked(wails.notices).mockResolvedValue({
      notices: [], dismissed: 0, failed: ["accounting.ledger"],
    });

    renderApp(<NoticeCentre />);

    // Silence reads as "nothing is wrong". A user seeing an empty centre while the ledger check
    // is broken would conclude the books are fine.
    expect(await screen.findByText("Some checks could not run")).toBeInTheDocument();
  });

  it("shows how many are hidden, so there is a way back", async () => {
    vi.mocked(wails.notices).mockResolvedValue({
      notices: [], dismissed: 3, failed: [],
    });

    renderApp(<NoticeCentre />);

    expect(await screen.findByText("3 hidden")).toBeInTheDocument();
  });

  it("renders a notice from its key and parameters", async () => {
    vi.mocked(wails.notices).mockResolvedValue({
      notices: [
        {
          key: "ops.backup.never", rule: "ops.backup", severity: "warning",
          messageKey: "notices.backup.never", params: {}, entityId: "", entityKind: "",
        },
      ],
      dismissed: 0, failed: [],
    });

    renderApp(<NoticeCentre />);

    // Translated on the FRONTEND. A rendered sentence crossing the boundary would put i18n in two
    // places, and the backend has no locale.
    expect(
      await screen.findByText("This company has never been backed up."),
    ).toBeInTheDocument();
  });
});

describe("backups", () => {
  it("lists a backup from a newer build and says why it cannot be used", async () => {
    vi.mocked(wails.backups).mockResolvedValue([
      {
        name: "on_demand-20260816T100000Z.db", takenAt: "2026-08-16T10:00:00Z",
        reason: "on_demand", schemaVersion: 41, sizeBytes: 204800,
        appVersion: "1.1.0", restorable: false,
      },
    ]);

    renderApp(<BackupScreen />);

    // Listed, not hidden: a user hunting for a backup they know they made is worse off than one
    // told why it cannot be used.
    expect(await screen.findByText("Made by a newer version")).toBeInTheDocument();
    expect(screen.queryByText("Restore")).not.toBeInTheDocument();
  });

  it("says a restart is needed and names the safety copy", async () => {
    vi.mocked(wails.pendingRestore).mockResolvedValue({
      from: "on_demand-20260816T100000Z.db",
      replacingVersion: 37, restoringVersion: 37,
      safetyBackup: "before_restore-20260816T110000Z.db",
      preparedAt: "2026-08-16T11:00:00Z",
    });

    renderApp(<BackupScreen />);

    expect(
      await screen.findByText("Close and reopen to finish restoring"),
    ).toBeInTheDocument();
    // The way back is on screen BEFORE it is needed.
    expect(
      screen.getByText(/before_restore-20260816T110000Z\.db/),
    ).toBeInTheDocument();
  });
});
