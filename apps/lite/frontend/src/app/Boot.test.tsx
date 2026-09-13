import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError, CODE_BRIDGE_UNAVAILABLE, CODE_CALL_FAILED } from "@/api/envelope";
import { Boot } from "./Boot";

const child = <p>application</p>;

describe("Boot", () => {
  it("renders the application once the backend is ready", async () => {
    renderWithProviders(<Boot>{child}</Boot>);
    expect(await screen.findByText("application")).toBeInTheDocument();
  });

  it("shows migration progress in Arabic while starting", async () => {
    const client = fakeClient({
      app: { bootStatus: async () => ({ state: "starting", phase: "migrating", current: 2, total: 5 }) },
    });
    renderWithProviders(<Boot>{child}</Boot>, { client });
    expect(await screen.findByText("يجري تحديث قاعدة البيانات…")).toBeInTheDocument();
    expect(screen.getByText("الخطوة 2 من 5")).toBeInTheDocument();
    expect(screen.queryByText("application")).not.toBeInTheDocument();
  });

  it("explains a failed start: the headline, the specific reason, and where the backup is", async () => {
    const client = fakeClient({
      app: {
        bootStatus: async () => ({
          state: "failed",
          phase: "",
          current: 0,
          total: 0,
          error: { code: "lite.bootstrap.migration_failed", messageKey: "lite.bootstrap.migration_failed" },
          reason: { code: "migrate.insufficient_disk", messageKey: "migrate.insufficient_disk" },
          backupPath: "C:\\Users\\محمد\\AppData\\Local\\Mizan Lite\\backups\\before_migration.db",
        }),
      },
    });
    renderWithProviders(<Boot>{child}</Boot>, { client, locale: "en" });

    expect(await screen.findByRole("heading", { name: "Mizan Lite could not start" })).toBeInTheDocument();
    expect(screen.getByText("The database update failed, so Mizan Lite cannot start.")).toBeInTheDocument();
    expect(screen.getByText(/There is not enough free disk space/)).toBeInTheDocument();
    const path = screen.getByText(/before_migration\.db/);
    expect(path.tagName).toBe("BDI");
    expect(path).toHaveAttribute("dir", "ltr");
    expect(screen.queryByText("application")).not.toBeInTheDocument();
  });

  it("says plainly that there is no backend, rather than showing anything else", async () => {
    const client = fakeClient({
      app: {
        bootStatus: async () => {
          throw new BindingError({ code: CODE_BRIDGE_UNAVAILABLE, messageKey: CODE_BRIDGE_UNAVAILABLE });
        },
      },
    });
    renderWithProviders(<Boot>{child}</Boot>, { client, locale: "en" });
    expect(await screen.findByRole("heading", { name: "The application cannot reach its engine" })).toBeInTheDocument();
    expect(screen.queryByText("application")).not.toBeInTheDocument();
  });

  it("keeps polling through a transient failure and then starts", async () => {
    let calls = 0;
    const bootStatus = vi.fn(async () => {
      calls += 1;
      if (calls === 1) throw new BindingError({ code: CODE_CALL_FAILED, messageKey: CODE_CALL_FAILED });
      return { state: "ready", phase: "done", current: 0, total: 0 };
    });
    renderWithProviders(<Boot>{child}</Boot>, { client: fakeClient({ app: { bootStatus } }) });
    expect(await screen.findByText("application")).toBeInTheDocument();
    expect(bootStatus).toHaveBeenCalledTimes(2);
  });

  it("stops polling once it has an answer", async () => {
    const bootStatus = vi.fn(async () => ({ state: "ready", phase: "done", current: 0, total: 0 }));
    renderWithProviders(<Boot>{child}</Boot>, { client: fakeClient({ app: { bootStatus } }) });
    await screen.findByText("application");
    await new Promise((resolve) => setTimeout(resolve, 400));
    await waitFor(() => expect(bootStatus).toHaveBeenCalledTimes(1));
  });
});
