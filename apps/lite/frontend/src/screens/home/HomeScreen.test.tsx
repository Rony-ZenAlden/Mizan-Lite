import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aBackup, aBackupStatus, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { HomeScreen } from "./HomeScreen";

describe("HomeScreen", () => {
  it("shows what the frontend reached, in Arabic with Latin digits", async () => {
    const client = fakeClient({
      app: {
        health: async () => ({ version: "0.1.0", schemaVersion: 12, platform: "windows", dataDir: "C:\\Data\\Mizan Lite" }),
      },
    });
    renderWithProviders(<HomeScreen />, { client, locale: "ar" });

    expect(await screen.findByText("متصل بمحرّك التطبيق")).toBeInTheDocument();
    expect(screen.getByText("ويندوز")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
    const dir = screen.getByText("C:\\Data\\Mizan Lite");
    expect(dir.tagName).toBe("BDI");
    expect(dir).toHaveAttribute("dir", "ltr");
  });

  it("names macOS in English", async () => {
    renderWithProviders(<HomeScreen />, { locale: "en" });
    expect(await screen.findByText("macOS")).toBeInTheDocument();
  });

  it("translates a failure and retries on request", async () => {
    let attempt = 0;
    const health = vi.fn(async () => {
      attempt += 1;
      if (attempt === 1) throw new BindingError({ code: "lite.api.not_ready", messageKey: "lite.api.not_ready" });
      return { version: "0.1.0", schemaVersion: 1, platform: "darwin", dataDir: "/d" };
    });
    renderWithProviders(<HomeScreen />, { client: fakeClient({ app: { health } }), locale: "en" });

    expect(await screen.findByRole("alert")).toHaveTextContent("The application is still starting");
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Connected to the application engine")).toBeInTheDocument();
    expect(health).toHaveBeenCalledTimes(2);
  });

  it("shows the last backup and the last outside copy with their ages (Q-L7.8)", async () => {
    renderWithProviders(<HomeScreen />, { locale: "en" });
    const status = await screen.findByTestId("backup-status");
    expect(status).toHaveTextContent("Last backup3 hours ago");
    expect(status).toHaveTextContent("Daily");
    expect(status).toHaveTextContent("Last outside copy3 hours ago");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Backups" })).toHaveAttribute("href", "/backups");
  });

  it("warns when the outside copy is more than two days old, in Arabic", async () => {
    const status = async () => aBackupStatus({ outsideStale: true, outsideFailed: "lite.backups.folder_missing", lastOutside: aBackup({ ageSeconds: 3 * 86_400 }) });
    renderWithProviders(<HomeScreen />, { client: fakeClient({ backups: { status } }), locale: "ar" });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("آخر نسخة خارجية قبل 3 أيام");
    expect(alert).toHaveTextContent("لا يمكن الوصول إلى المجلد الخارجي");
  });

  it("says when the last outside copy failed even if it is not yet old, and when nothing was ever copied", async () => {
    const failed = async () => aBackupStatus({ outsideFailed: "lite.backups.copy_failed" });
    const { unmount } = renderWithProviders(<HomeScreen />, { client: fakeClient({ backups: { status: failed } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("The last copy to the outside folder failed");
    unmount();

    const never = async () => aBackupStatus({ outsideStale: true, lastOutside: undefined, last: undefined });
    renderWithProviders(<HomeScreen />, { client: fakeClient({ backups: { status: never } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("Nothing has been copied to the outside folder yet");
    expect(screen.getByTestId("backup-status")).toHaveTextContent("Last backupNone");
  });

  it("says a restore was applied at this start, and no outside folder when none is chosen", async () => {
    const status = async () =>
      aBackupStatus({ folder: "", lastOutside: undefined, restored: { from: "/data/backups/scheduled-20260912T180000Z.db", safety: "before_restore-20260914T090000Z.db" } });
    renderWithProviders(<HomeScreen />, { client: fakeClient({ backups: { status } }), locale: "en" });
    const restored = await screen.findByText("A backup was restored at this start");
    expect(restored.parentElement).toHaveTextContent("/data/backups/scheduled-20260912T180000Z.db");
    expect(screen.getByTestId("backup-status")).toHaveTextContent("Last outside copyNo outside folder chosen");
  });

  it("a backup status that cannot be read is said, not hidden", async () => {
    const status = async () => {
      throw new BindingError({ code: "lite.api.not_ready", messageKey: "lite.api.not_ready" });
    };
    renderWithProviders(<HomeScreen />, { client: fakeClient({ backups: { status } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("The application is still starting");
    expect(await screen.findByText("Connected to the application engine")).toBeInTheDocument();
  });
});
