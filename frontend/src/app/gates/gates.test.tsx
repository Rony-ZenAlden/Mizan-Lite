import { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthGate } from "@/app/gates/AuthGate";
import { SetupGate } from "@/app/gates/SetupGate";
import { useSessionStore } from "@/app/session/session";
import { useSignOut } from "@/app/session/useSignOut";
import { renderApp, SIGNED_IN, SIGNED_OUT } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    setupStatus: vi.fn(),
    me: vi.fn(),
    preferences: vi.fn(),
    logout: vi.fn(),
    login: vi.fn(),
    applySetup: vi.fn(),
  };
});

const mocked = vi.mocked(wails);

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en",
    theme: "system",
    availableLocales: ["en", "ar"], landing: "/",
  });
  useSessionStore.getState().clear();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("SetupGate", () => {
  it("renders nothing below it while the answer is unknown", async () => {
    // A gate is a MOUNT BOUNDARY, not a redirect (D1). If children rendered here, the shell
    // would mount, fire its queries against a database with no company, and unmount a tick
    // later — a visible flash plus a burst of failures.
    let resolve: (value: wails.SetupStatus) => void = () => undefined;
    mocked.setupStatus.mockReturnValue(
      new Promise<wails.SetupStatus>((r) => {
        resolve = r;
      }),
    );

    renderApp(
      <SetupGate>
        <p>the application</p>
      </SetupGate>,
    );

    expect(await screen.findByRole("status")).toBeInTheDocument();
    expect(screen.queryByText("the application")).not.toBeInTheDocument();

    resolve({ required: false });
    expect(await screen.findByText("the application")).toBeInTheDocument();
  });

  it("renders the wizard rather than the application when setup is required", async () => {
    mocked.setupStatus.mockResolvedValue({
      required: true,
      options: { countries: [], businessProfiles: [], currencies: [], locales: ["en"] },
    });

    renderApp(
      <SetupGate>
        <p>the application</p>
      </SetupGate>,
    );

    // The wizard's FIRST step, whatever it is called. Since 10.16 that is "Your shop" — the
    // seven steps collapsed to three, and language moved into the country's defaults.
    expect(await screen.findByRole("heading", { name: /your shop/i })).toBeInTheDocument();
    expect(screen.queryByText("the application")).not.toBeInTheDocument();
  });

  it("reports a failure rather than guessing which side to show", async () => {
    // Neither branch is safe when the question is unanswerable: the wizard would offer to
    // re-create an existing company, and the shell would query a database that may have none.
    mocked.setupStatus.mockRejectedValue(
      new wails.BindingError({ code: "app.call_failed", messageKey: "app.call_failed" }),
    );

    renderApp(
      <SetupGate>
        <p>the application</p>
      </SetupGate>,
    );

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByText("the application")).not.toBeInTheDocument();
  });
});

describe("AuthGate", () => {
  it("renders the login screen when signed out", async () => {
    mocked.me.mockResolvedValue(SIGNED_OUT);

    renderApp(
      <AuthGate>
        <p>the application</p>
      </AuthGate>,
    );

    expect(await screen.findByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.queryByText("the application")).not.toBeInTheDocument();
  });

  it("renders its children when signed in", async () => {
    mocked.me.mockResolvedValue(SIGNED_IN);

    renderApp(
      <AuthGate>
        <p>the application</p>
      </AuthGate>,
    );

    expect(await screen.findByText("the application")).toBeInTheDocument();
  });

  it("stops a user whose password must change, rather than letting them through", async () => {
    // must_change exists for one situation: an administrator set this person's password, so
    // another human knows their credential. Letting them through "temporarily" would make the
    // flag a lie exactly when it matters (D6).
    mocked.me.mockResolvedValue({ ...SIGNED_IN, mustChange: true });

    renderApp(
      <AuthGate>
        <p>the application</p>
      </AuthGate>,
    );

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.queryByText("the application")).not.toBeInTheDocument();
  });

  it("publishes the principal to the session store", async () => {
    mocked.me.mockResolvedValue(SIGNED_IN);

    renderApp(
      <AuthGate>
        <p>the application</p>
      </AuthGate>,
    );

    await screen.findByText("the application");
    await waitFor(() => {
      expect(useSessionStore.getState().session?.username).toBe("nadia");
    });
  });
});

describe("signing out", () => {
  it("clears the query cache", async () => {
    // Without this, the next cashier to sign in at a shared terminal sees the previous one's
    // cached lists until each query happens to refetch. The backend cannot prevent it: a
    // cached answer never reaches a binding (D3).
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["users", "list"], [{ id: "1", username: "someone-elses-data" }]);

    mocked.me.mockResolvedValue(SIGNED_IN);
    mocked.logout.mockResolvedValue(true);

    const { user } = renderApp(
      <AuthGate>
        <SignOutButton />
      </AuthGate>,
      { client },
    );

    await screen.findByRole("button", { name: /sign out/i });
    expect(client.getQueryData(["users", "list"])).toBeDefined();

    await user.click(screen.getByRole("button", { name: /sign out/i }));

    await waitFor(() => {
      expect(client.getQueryData(["users", "list"])).toBeUndefined();
    });
  });
});

/** The smallest thing that signs out, so the test covers the hook rather than the shell. */
function SignOutButton() {
  const signOut = useSignOut();
  return (
    <button type="button" onClick={() => signOut.mutate()}>
      Sign out
    </button>
  );
}
