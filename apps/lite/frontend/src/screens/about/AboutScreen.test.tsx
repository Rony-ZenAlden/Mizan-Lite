import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aBackup, aBackupStatus, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { AboutScreen } from "./AboutScreen";

describe("AboutScreen", () => {
  it("shows the version, the database version, the system and the data folder, in Arabic with Latin digits", async () => {
    const showInFolder = vi.fn(async () => true);
    renderWithProviders(<AboutScreen />, { client: fakeClient({ exports: { showInFolder } }), locale: "ar" });
    expect(await screen.findByText("0.9.0")).toBeInTheDocument();
    expect(screen.getByText("ويندوز")).toBeInTheDocument();
    expect(screen.getByText("8")).toBeInTheDocument();
    const dir = screen.getByText("C:\\Users\\shop\\AppData\\Local\\Mizan Lite");
    expect(dir.tagName).toBe("BDI");
    expect(dir).toHaveAttribute("dir", "ltr");
    await userEvent.click(screen.getAllByRole("button", { name: "إظهار في المجلد" })[0]!);
    expect(showInFolder).toHaveBeenCalledWith("C:\\Users\\shop\\AppData\\Local\\Mizan Lite");
  });

  it("saves a guide as a PDF and shows where", async () => {
    const saveGuide = vi.fn(async (name: string) => ({ path: `/Users/shop/Documents/${name}.pdf`, bytes: 1, cancelled: false }));
    renderWithProviders(<AboutScreen />, { client: fakeClient({ app: { saveGuide } }), locale: "en" });
    await userEvent.click(await screen.findByRole("button", { name: "Shop guide (Arabic)" }));
    expect(saveGuide).toHaveBeenCalledWith("shop-guide-ar");
    expect(await screen.findByText("/Users/shop/Documents/shop-guide-ar.pdf")).toBeInTheDocument();
    expect(screen.getByTestId("notices")).toHaveTextContent("SIL Open Font License");
  });

  it("saves a support file without the database unless it is ticked, and the database only through the PIN", async () => {
    const saveSupportFile = vi
      .fn(async () => ({ path: "/Users/shop/support.zip", bytes: 1, cancelled: false }))
      .mockImplementationOnce(async () => ({ path: "/Users/shop/support.zip", bytes: 1, cancelled: false }))
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      });
    renderWithProviders(<AboutScreen />, { client: fakeClient({ app: { saveSupportFile } }), locale: "en" });
    await userEvent.click(await screen.findByRole("button", { name: "Save a support file…" }));
    expect(saveSupportFile).toHaveBeenLastCalledWith(false);
    await userEvent.click(screen.getByLabelText("Include a copy of the database"));
    expect(screen.getByText(/holds customers' names, phones and debts/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Save a support file…" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await screen.findByText("/Users/shop/support.zip");
    expect(saveSupportFile).toHaveBeenCalledTimes(3);
    expect(saveSupportFile).toHaveBeenLastCalledWith(true);
  });

  it("translates a failure to read and retries on request", async () => {
    let attempt = 0;
    const about = vi.fn(async () => {
      attempt += 1;
      if (attempt === 1) throw new BindingError({ code: "lite.api.not_ready", messageKey: "lite.api.not_ready" });
      return { version: "0.9.0", schemaVersion: 8, platform: "darwin", arch: "arm64", dataDir: "/d", logsDir: "/d/logs", backupsDir: "/d/backups", guides: [], notices: "" };
    });
    renderWithProviders(<AboutScreen />, { client: fakeClient({ app: { about } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("The application is still starting");
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("macOS")).toBeInTheDocument();
    expect(about).toHaveBeenCalledTimes(2);
  });

  it("shows the last backup and the last outside copy with their ages (Q-L7.8)", async () => {
    renderWithProviders(<AboutScreen />, { locale: "en" });
    const status = await screen.findByTestId("backup-status");
    expect(status).toHaveTextContent("Last backup3 hours ago");
    expect(status).toHaveTextContent("Daily");
    expect(status).toHaveTextContent("Last outside copy3 hours ago");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Backups" })).toHaveAttribute("href", "/backups");
  });

  it("warns when the outside copy is more than two days old, in Arabic", async () => {
    const status = async () => aBackupStatus({ outsideStale: true, outsideFailed: "lite.backups.folder_missing", lastOutside: aBackup({ ageSeconds: 3 * 86_400 }) });
    renderWithProviders(<AboutScreen />, { client: fakeClient({ backups: { status } }), locale: "ar" });
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveReadableText("آخر نسخة خارجية قبل 3 أيام");
    expect(alert).toHaveReadableText("لا يمكن الوصول إلى المجلد الخارجي");
  });

  it("says when the last outside copy failed even if it is not yet old, and when nothing was ever copied", async () => {
    const failed = async () => aBackupStatus({ outsideFailed: "lite.backups.copy_failed" });
    const { unmount } = renderWithProviders(<AboutScreen />, { client: fakeClient({ backups: { status: failed } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("The last copy to the outside folder failed");
    unmount();

    const never = async () => aBackupStatus({ outsideStale: true, lastOutside: undefined, last: undefined });
    renderWithProviders(<AboutScreen />, { client: fakeClient({ backups: { status: never } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("Nothing has been copied to the outside folder yet");
    expect(screen.getByTestId("backup-status")).toHaveTextContent("Last backupNone");
  });

  it("says a restore was applied at this start, and no outside folder when none is chosen", async () => {
    const status = async () =>
      aBackupStatus({ folder: "", lastOutside: undefined, restored: { from: "/data/backups/scheduled-20260912T180000Z.db", safety: "before_restore-20260914T090000Z.db" } });
    renderWithProviders(<AboutScreen />, { client: fakeClient({ backups: { status } }), locale: "en" });
    const restored = await screen.findByText("A backup was restored at this start");
    expect(restored.parentElement).toHaveTextContent("/data/backups/scheduled-20260912T180000Z.db");
    expect(screen.getByTestId("backup-status")).toHaveTextContent("Last outside copyNo outside folder chosen");
  });

  it("a backup status that cannot be read is said, not hidden", async () => {
    const status = async () => {
      throw new BindingError({ code: "lite.api.not_ready", messageKey: "lite.api.not_ready" });
    };
    renderWithProviders(<AboutScreen />, { client: fakeClient({ backups: { status } }), locale: "en" });
    expect(await screen.findByRole("alert")).toHaveTextContent("The application is still starting");
    expect(await screen.findByText("0.9.0")).toBeInTheDocument();
  });
});
