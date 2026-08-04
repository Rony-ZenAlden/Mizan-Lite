import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { JobStatusPanel } from "./JobStatusPanel";
import { PreferencesProvider } from "@/app/providers/PreferencesProvider";
import { installBridge } from "@/lib/wails/testing";
import { renderIn } from "@/test/render";

function ok(data: unknown) {
  return Promise.resolve({ ok: true, data });
}

const PREFS = {
  Preferences: () => ok({ locale: "en", theme: "light", availableLocales: ["en", "ar"] }),
};

function renderPanel() {
  return renderIn(
    <PreferencesProvider>
      <JobStatusPanel />
    </PreferencesProvider>,
  );
}

const JOB = {
  key: "platform.heartbeat",
  enabled: true,
  schedule: "@every 1m",
  lastRunAt: "2026-08-04T09:00:00.000Z",
  nextRunAt: "2026-08-04T09:01:00.000Z",
  lastStatus: "succeeded",
};

// The first screen to exercise all four designed states for real (§FE.2): loading, error,
// empty, populated. §24.2: "invisible background failures are how customers lose backups
// without knowing" — which is why this panel exists in Phase 0 at all.

describe("JobStatusPanel", () => {
  it("shows a loading state before the data arrives", async () => {
    // A never-resolving promise holds the panel in its loading state.
    installBridge({ Config: PREFS, Ops: { Jobs: () => new Promise(() => {}) } });
    renderPanel();

    // Awaited so the preferences load settles inside act(); the panel itself stays loading.
    await waitFor(() => expect(document.querySelector('[aria-busy="true"]')).toBeInTheDocument());
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("shows an empty state when no jobs are registered", async () => {
    installBridge({ Config: PREFS, Ops: { Jobs: () => ok([]) } });
    renderPanel();
    expect(await screen.findByText("No background jobs")).toBeInTheDocument();
  });

  it("renders an error from its code, not from prose", async () => {
    installBridge({
      Config: PREFS,
      Ops: {
        Jobs: () =>
          Promise.resolve({
            ok: false,
            data: null,
            error: { code: "app.not_ready", messageKey: "app.not_ready" },
          }),
      },
    });
    renderPanel();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Mizan is still starting. Please wait a moment.",
    );
  });

  it("lists jobs with their schedule and last result", async () => {
    installBridge({ Config: PREFS, Ops: { Jobs: () => ok([JOB]) } });
    renderPanel();

    expect(await screen.findByText("platform.heartbeat")).toBeInTheDocument();
    expect(screen.getByText("@every 1m")).toBeInTheDocument();
    expect(screen.getByText("Succeeded")).toBeInTheDocument();
  });

  it("shows a dash for a job that has never run", async () => {
    installBridge({
      Config: PREFS,
      Ops: { Jobs: () => ok([{ ...JOB, lastRunAt: "", lastStatus: "" }]) },
    });
    renderPanel();

    await screen.findByText("platform.heartbeat");
    // The zero time must not render as the year 1: empty is the honest answer.
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.queryByText(/0001/)).not.toBeInTheDocument();
  });

  it("loads recent runs when a job row is selected", async () => {
    const user = userEvent.setup();
    installBridge({
      Config: PREFS,
      Ops: {
        Jobs: () => ok([JOB]),
        Runs: () =>
          ok([
            {
              runId: "run-1",
              jobKey: "platform.heartbeat",
              startedAt: "2026-08-04T09:00:00.000Z",
              finishedAt: "2026-08-04T09:00:00.010Z",
              status: "succeeded",
              attempt: 1,
              error: "",
              output: "",
              triggeredBy: "schedule",
            },
          ]),
      },
    });
    renderPanel();

    await user.click(await screen.findByText("platform.heartbeat"));

    await waitFor(() =>
      expect(screen.getByText(/Recent runs/)).toBeInTheDocument(),
    );
    expect(screen.getByText("schedule")).toBeInTheDocument();
  });

  it("keeps a selected job's rows reachable by keyboard", async () => {
    const user = userEvent.setup();
    installBridge({ Config: PREFS, Ops: { Jobs: () => ok([JOB]), Runs: () => ok([]) } });
    renderPanel();

    await screen.findByText("platform.heartbeat");
    const row = screen.getByRole("button", { name: /platform.heartbeat/ });
    row.focus();
    await user.keyboard("{Enter}");

    // A mouse-only table is unacceptable in a keyboard-first product (§31).
    expect(await screen.findByText("No runs yet")).toBeInTheDocument();
  });
});
