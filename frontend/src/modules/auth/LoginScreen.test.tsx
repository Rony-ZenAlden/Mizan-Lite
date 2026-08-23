import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LoginScreen } from "@/modules/auth/LoginScreen";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return { ...actual, login: vi.fn(), me: vi.fn(), preferences: vi.fn(), setLocale: vi.fn() };
});

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en",
    theme: "system",
    availableLocales: ["en", "ar"], landing: "/",
  });
});

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

async function fillAndSubmit(user: ReturnType<typeof renderApp>["user"]) {
  await user.type(screen.getByLabelText(/username/i), "nadia");
  await user.type(screen.getByLabelText(/^password$/i), "a sufficiently long passphrase");
  await user.click(screen.getByRole("button", { name: /sign in/i }));
}

describe("LoginScreen", () => {
  it("signs in and seeds the session cache", async () => {
    vi.mocked(wails.login).mockResolvedValue(SIGNED_IN);

    const { user, client } = renderApp(<LoginScreen />);
    await fillAndSubmit(user);

    await waitFor(() => {
      expect(client.getQueryData(["auth", "me"])).toEqual(SIGNED_IN);
    });
    // Seeded rather than invalidated: the answer is in hand, and a refetch would put an
    // avoidable round trip between "signed in" and the first screen appearing.
    expect(wails.me).not.toHaveBeenCalled();
  });

  it("renders the failure from its CODE, not from prose", async () => {
    // The backend never returns display text (§22.2). If this rendered `error.message`, the
    // user would see "binding failed: identity.invalid_credentials" — in English, always.
    vi.mocked(wails.login).mockRejectedValue(
      new wails.BindingError({
        code: "identity.invalid_credentials",
        messageKey: "identity.invalid_credentials",
      }),
    );

    const { user } = renderApp(<LoginScreen />);
    await fillAndSubmit(user);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/username or password/i);
    expect(alert).not.toHaveTextContent("identity.invalid_credentials");
  });

  it("shows how long to wait when the account is throttled", async () => {
    // §13.1's lockout is "delay, never lock". Telling someone to try again later without
    // saying how much later is the difference between a working shop and a support call.
    vi.mocked(wails.login).mockRejectedValue(
      new wails.BindingError({
        code: "identity.too_many_attempts",
        messageKey: "identity.too_many_attempts",
        params: { retryAfter: "30" },
      }),
    );

    const { user } = renderApp(<LoginScreen />);
    await fillAndSubmit(user);

    const submit = await screen.findByRole("button", { name: /try again in 30s/i });
    expect(submit).toBeDisabled();
  });

  it("puts a per-field failure on the field it belongs to", async () => {
    // The envelope has carried `fields` since Step 0.11 and nothing consumed it until now.
    // This is that seam's first real use — which, in this project, is where a seam turns out
    // to be wrong if it is.
    vi.mocked(wails.login).mockRejectedValue(
      new wails.BindingError({
        code: "identity.password_required",
        messageKey: "identity.password_required",
        fields: [
          {
            field: "password",
            code: "identity.password_required",
            messageKey: "identity.password_required",
          },
        ],
      }),
    );

    const { user } = renderApp(<LoginScreen />);
    await fillAndSubmit(user);

    await waitFor(() => {
      expect(screen.getByLabelText(/^password$/i)).toHaveAttribute("aria-invalid", "true");
    });
    expect(screen.getByLabelText(/username/i)).not.toHaveAttribute("aria-invalid");
  });

  it("offers a language picker, because someone who cannot read it cannot sign in", async () => {
    vi.mocked(wails.login).mockResolvedValue(SIGNED_IN);
    renderApp(<LoginScreen />);
    expect(
      await screen.findByRole("combobox", { name: /language/i }),
    ).toBeInTheDocument();
  });
});
