import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { BootGate } from "./BootGate";
import { installBridge, installRuntime } from "@/lib/wails/testing";
import { renderIn } from "@/test/render";
import type { BootStatus } from "@/lib/wails";

function status(overrides: Partial<BootStatus> = {}): BootStatus {
  return {
    state: "starting",
    phase: "checking",
    current: 0,
    total: 0,
    name: "",
    error: null,
    backupPath: "",
    ...overrides,
  };
}

function installStatus(value: BootStatus | (() => BootStatus)) {
  installBridge({
    Boot: {
      Status: () =>
        Promise.resolve({ ok: true, data: typeof value === "function" ? value() : value }),
    },
  });
}

describe("BootGate", () => {
  it("shows the boot screen while the graph is being built", async () => {
    installStatus(status({ phase: "migrating" }));
    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    expect(await screen.findByText("Updating your database…")).toBeInTheDocument();
    // The shell must NOT mount before ready: Step 0.10's ordering guarantee is that nothing
    // queries a database that may be mid-restore, and this component is what preserves it now.
    expect(screen.queryByText("shell")).not.toBeInTheDocument();
  });

  it("shows migration progress only when there is something to report", async () => {
    installStatus(status({ phase: "migrating", current: 2, total: 3 }));
    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    const bar = await screen.findByRole("progressbar");
    expect(bar).toHaveAttribute("aria-valuenow", "2");
    expect(bar).toHaveAttribute("aria-valuemax", "3");
    expect(screen.getByText("Step 2 of 3")).toBeInTheDocument();
  });

  it("hides the progress bar when nothing is pending", async () => {
    installStatus(status({ phase: "checking" }));
    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    await screen.findByText("Checking your database…");
    // A bar reading "0 of 0" on every launch is noise that trains the user to ignore it.
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("mounts the shell once boot reports ready", async () => {
    installStatus(status({ state: "ready", phase: "done" }));
    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    expect(await screen.findByText("shell")).toBeInTheDocument();
  });

  it("renders a failure from its code, with the backup path", async () => {
    installStatus(
      status({
        state: "failed",
        error: {
          code: "migrate.migration_failed",
          messageKey: "migrate.migration_failed",
          params: { migration: "0003_currency.sql" },
        },
        backupPath: "/data/backups/pre-migration-1.db",
      }),
    );
    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    expect(await screen.findByText("Mizan could not start")).toBeInTheDocument();
    expect(screen.getByText("/data/backups/pre-migration-1.db")).toBeInTheDocument();
    expect(screen.queryByText("shell")).not.toBeInTheDocument();
  });

  it("renders the failure in Arabic when the OS language is Arabic", async () => {
    // The boot screen renders before `ui.locale` is readable, so this is the one screen whose
    // language comes from the OS. A first launch in Syria must not migrate in English.
    installStatus(status({ state: "failed", error: null }));
    renderIn(
      <BootGate locale="ar">
        <p>shell</p>
      </BootGate>,
      { locale: "ar" },
    );

    expect(await screen.findByText("تعذّر بدء تشغيل ميزان")).toBeInTheDocument();
    expect(document.documentElement.dir).toBe("rtl");
  });

  it("updates when a boot event arrives", async () => {
    const runtime = installRuntime();
    let current = status({ phase: "checking" });
    installStatus(() => current);

    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );
    await screen.findByText("Checking your database…");

    current = status({ state: "ready", phase: "done" });
    runtime.emit("boot:ready");

    expect(await screen.findByText("shell")).toBeInTheDocument();
  });

  it("polls on mount, so a boot that finished before the window loaded is not missed", async () => {
    // Events fired before the webview subscribed are lost, and on a warm database that is the
    // common case rather than the edge. Without the mount poll the app would hang on the boot
    // screen forever — with no runtime installed here, the poll is the ONLY signal.
    installStatus(status({ state: "ready", phase: "done" }));

    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    expect(await screen.findByText("shell")).toBeInTheDocument();
  });

  it("stays on the boot screen when the status probe fails", async () => {
    installBridge({ Boot: { Status: () => Promise.reject(new Error("bridge gone")) } });
    renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    // We genuinely do not know whether the app is ready; mounting the shell would be a guess.
    await waitFor(() => expect(screen.queryByText("shell")).not.toBeInTheDocument());
    expect(screen.getByText("Checking your database…")).toBeInTheDocument();
  });

  it("never shows developer prose from a failed boot", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    installStatus(
      status({
        state: "failed",
        error: { code: "bootstrap.startup_failed", messageKey: "bootstrap.startup_failed" },
      }),
    );
    const { container } = renderIn(
      <BootGate locale="en">
        <p>shell</p>
      </BootGate>,
    );

    await screen.findByText("Mizan could not start");
    // The rendered text comes from the catalog; the raw code must not leak onto the screen.
    expect(container.textContent).not.toContain("bootstrap.startup_failed");
    spy.mockRestore();
  });
});
