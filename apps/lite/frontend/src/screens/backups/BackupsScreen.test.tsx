import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { aBackup, aBackupStatus, fakeClient, renderWithProviders } from "@/api/testing";
import type { BackupInfo, BackupStatus, Loss } from "@/api/client";
import { BackupsScreen } from "./BackupsScreen";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });

/** Fails once as Go does outside owner mode, then answers. */
function guarded<A extends unknown[], T>(answer: (...args: A) => Promise<T>) {
  return vi.fn(answer).mockImplementationOnce(async () => {
    throw required();
  });
}

async function enterPin() {
  await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await settle();
}

describe("BackupsScreen", () => {
  it("lists every backup newest first with why, its age by Go's clock, its size and whether it is outside", async () => {
    renderWithProviders(<BackupsScreen />, { locale: "en" });
    await settle();
    const rows = screen.getAllByTestId("backup-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Daily");
    expect(rows[0]).toHaveTextContent("3 hours ago");
    expect(rows[0]).toHaveTextContent("2.3 MB");
    expect(rows[0]).toHaveTextContent("Yes");
    expect(rows[1]).toHaveTextContent("On close");
    expect(rows[1]).toHaveTextContent("No");
    expect(screen.getByText("/Volumes/USB")).toBeInTheDocument();
  });

  it("anyone backs up now; the list is read again", async () => {
    const list = vi.fn(async () => [aBackup()]);
    const takeNow = vi.fn(async () => aBackup({ name: "on_demand-1.db", reason: "on_demand", outside: true }));
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { list, takeNow } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Back up now" }));
    await settle();
    expect(takeNow).toHaveBeenCalledTimes(1);
    expect(list).toHaveBeenCalledTimes(2);
    expect(screen.getByText("A backup was taken and copied to the outside folder")).toBeInTheDocument();
  });

  it("choosing the outside folder is the owner's", async () => {
    const setOutsideFolder = guarded(async (clear: boolean): Promise<BackupStatus> => aBackupStatus({ folder: clear ? "" : "/Volumes/SHOP" }));
    const status = vi.fn(async () => aBackupStatus({ folder: "", lastOutside: undefined }));
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { setOutsideFolder, status } }), locale: "en" });
    await settle();
    expect(screen.getAllByText("No outside folder chosen").length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("button", { name: "Choose a folder…" }));
    await enterPin();
    expect(setOutsideFolder).toHaveBeenCalledTimes(2);
    expect(setOutsideFolder).toHaveBeenLastCalledWith(false);
  });

  it("a chosen folder can be changed or cleared", async () => {
    const setOutsideFolder = vi.fn(async (clear: boolean): Promise<BackupStatus> => aBackupStatus({ folder: clear ? "" : "/Volumes/SHOP" }));
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { setOutsideFolder } }), locale: "en" });
    await settle();
    expect(screen.getByRole("button", { name: "Change the folder…" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Stop copying outside" }));
    await settle();
    expect(setOutsideFolder).toHaveBeenCalledWith(true);
  });

  it("a restore says what it removes, then restores and restarts", async () => {
    const lossPreview = guarded(
      async (name: string): Promise<Loss> => ({ backup: aBackup({ name }), sales: 214, voids: 0, debtEntries: 9, cashEntries: 3, stockMovements: 31 }),
    );
    const restore = vi.fn(async () => ({ staged: true, restarting: true }));
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { lossPreview, restore } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByTestId("backup-row")[0]!).getByRole("button", { name: "Restore" }));
    await enterPin();

    const dialog = screen.getByRole("dialog", { name: "Restore a backup" });
    expect(within(dialog).getByTestId("restore-when")).toHaveTextContent("This backup is from");
    const loss = within(dialog).getByTestId("restore-loss");
    expect(loss).toHaveTextContent("What was recorded since then will be removed:");
    expect(loss).toHaveTextContent("Sales: 214");
    expect(loss).toHaveTextContent("Debt entries (payments and others): 9");
    expect(loss).toHaveTextContent("Cash drawer entries: 3");
    expect(loss).toHaveTextContent("Stock movements: 31");
    expect(loss).not.toHaveTextContent("Voids");
    expect(dialog).toHaveTextContent("A copy of the current data is taken first, then the application restarts.");
    expect(restore).not.toHaveBeenCalled();

    await userEvent.click(within(dialog).getByRole("button", { name: "Restore and restart" }));
    await settle();
    expect(restore).toHaveBeenCalledWith(aBackup().name);
    expect(within(dialog).getByRole("status")).toHaveTextContent("Restarting to apply the restore…");
  });

  it("a restore can be cancelled, and nothing lost is said plainly in Arabic", async () => {
    const lossPreview = vi.fn(async (name: string): Promise<Loss> => ({ backup: aBackup({ name }), sales: 0, voids: 0, debtEntries: 0, cashEntries: 0, stockMovements: 0 }));
    const restore = vi.fn(async () => ({ staged: true, restarting: true }));
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { lossPreview, restore } }), locale: "ar" });
    await settle();
    await userEvent.click(within(screen.getAllByTestId("backup-row")[0]!).getByRole("button", { name: "استعادة" }));
    await settle();
    const dialog = screen.getByRole("dialog", { name: "استعادة نسخة احتياطية" });
    expect(within(dialog).getByTestId("restore-loss")).toHaveTextContent("لم يُسجّل شيء بعد هذه النسخة.");
    await userEvent.click(within(dialog).getByRole("button", { name: "إلغاء" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(restore).not.toHaveBeenCalled();
  });

  it("restoring from a file verifies it first, then shows its warning; a closed dialog does nothing", async () => {
    let chosen = false;
    const restoreFromFile = vi.fn(async (): Promise<BackupInfo> => (chosen ? aBackup({ name: "imported-1.db", reason: "imported" }) : aBackup({ name: "" })));
    const lossPreview = vi.fn(async (name: string): Promise<Loss> => ({ backup: aBackup({ name, reason: "imported" }), sales: 5, voids: 1, debtEntries: 0, cashEntries: 0, stockMovements: 0 }));
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { restoreFromFile, lossPreview } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Restore from a file…" }));
    await settle();
    expect(lossPreview).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    chosen = true;
    await userEvent.click(screen.getByRole("button", { name: "Restore from a file…" }));
    await settle();
    expect(lossPreview).toHaveBeenCalledWith("imported-1.db");
    const dialog = screen.getByRole("dialog", { name: "Restore a backup" });
    expect(dialog).toHaveTextContent("From a file");
    expect(dialog).toHaveTextContent("Voids: 1");
  });

  it("saves a copy where the owner chooses; a file that is not a backup is refused in words", async () => {
    const saveCopy = vi.fn(async () => ({ path: "/Volumes/USB/copy.db", bytes: 0, cancelled: false }));
    const lossPreview = vi.fn(async (): Promise<Loss> => {
      throw new BindingError({ code: "lite.backups.not_a_backup", messageKey: "lite.backups.not_a_backup" });
    });
    renderWithProviders(<BackupsScreen />, { client: fakeClient({ backups: { saveCopy, lossPreview } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByTestId("backup-row")[1]!).getByRole("button", { name: "Save a copy…" }));
    await settle();
    expect(saveCopy).toHaveBeenCalledWith("on_close-20260913T190000Z.db");
    expect(screen.getByText("/Volumes/USB/copy.db")).toBeInTheDocument();

    await userEvent.click(within(screen.getAllByTestId("backup-row")[0]!).getByRole("button", { name: "Restore" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("That file is not a Mizan Lite backup.");
  });
});
