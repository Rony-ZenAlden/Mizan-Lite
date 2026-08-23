import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PreferencesProvider, usePreferences } from "./PreferencesProvider";
import { installBridge } from "@/lib/wails/testing";
import { renderIn } from "@/test/render";
import { Button } from "@/shared/ui";

function Probe() {
  const { locale, theme, resolvedTheme, setLocale, setTheme } = usePreferences();
  return (
    <div>
      <p data-testid="locale">{locale}</p>
      <p data-testid="theme">{theme}</p>
      <p data-testid="resolved">{resolvedTheme}</p>
      <Button onClick={() => void setLocale("ar")}>to-arabic</Button>
      <Button onClick={() => void setTheme("dark")}>to-dark</Button>
    </div>
  );
}

function ok(data: unknown) {
  return Promise.resolve({ ok: true, data });
}

describe("PreferencesProvider", () => {
  // <html> survives between tests in a file, so an attribute set by one test is still there for
  // the next. Without this the transition gate reads as already-open before it is opened.
  beforeEach(() => {
    delete document.documentElement.dataset.themeTransitions;
  });

  it("reads locale and theme from the backend, not from component state", async () => {
    installBridge({
      Config: {
        Preferences: () => ok({ locale: "ar", theme: "dark", availableLocales: ["en", "ar"], landing: "/" }),
      },
    });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("locale")).toHaveTextContent("ar"));
    expect(screen.getByTestId("theme")).toHaveTextContent("dark");
  });

  it("switches language with no reload, updating lang and dir", async () => {
    const user = userEvent.setup();
    const setLocale = vi.fn(() =>
      ok({ locale: "ar", theme: "system", availableLocales: ["en", "ar"], landing: "/" }),
    );
    installBridge({
      Config: {
        Preferences: () => ok({ locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/" }),
        SetLocale: setLocale,
      },
    });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("locale")).toHaveTextContent("en"));

    await user.click(screen.getByRole("button", { name: "to-arabic" }));

    // The write goes to the backend — §1.4: the frontend must not hold a private opinion
    // about the active language, or a printed document will disagree with the screen.
    await waitFor(() => expect(setLocale).toHaveBeenCalledWith("ar"));
    await waitFor(() => expect(document.documentElement.lang).toBe("ar"));
    expect(document.documentElement.dir).toBe("rtl");
  });

  /**
   * The important failure case.
   *
   * If the backend REFUSES a language (an unparseable tag, a permission denial in Phase 1) the
   * UI must not show it anyway. Otherwise the user believes they switched, and every document
   * the backend renders disagrees — the exact divergence §1.4 exists to close.
   */
  it("does not apply a language the backend refused", async () => {
    const user = userEvent.setup();
    installBridge({
      Config: {
        Preferences: () => ok({ locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/" }),
        SetLocale: () =>
          Promise.resolve({
            ok: false,
            data: null,
            error: { code: "i18n.invalid_locale", messageKey: "i18n.invalid_locale" },
          }),
      },
    });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("locale")).toHaveTextContent("en"));

    await user.click(screen.getByRole("button", { name: "to-arabic" }));

    // The rejection is reported...
    expect(await screen.findByText("That is not a valid language.")).toBeInTheDocument();
    // ...and the UI still shows what the backend actually holds.
    expect(screen.getByTestId("locale")).toHaveTextContent("en");
    expect(document.documentElement.lang).toBe("en");
  });

  it("falls back to defaults when preferences cannot be read", async () => {
    // An unreadable preference must never stop a shop from opening.
    installBridge({ Config: { Preferences: () => Promise.reject(new Error("not ready")) } });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("locale")).toHaveTextContent("en"));
  });

  it("resolves the 'system' theme against the OS preference", async () => {
    window.matchMedia = ((query: string) => ({
      matches: query.includes("dark"),
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      onchange: null,
      dispatchEvent: () => false,
    })) as typeof window.matchMedia;

    installBridge({
      Config: {
        Preferences: () => ok({ locale: "en", theme: "system", availableLocales: ["en"], landing: "/" }),
      },
    });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("resolved")).toHaveTextContent("dark"));
    // The SETTING stays "system" — storing the resolved value would freeze the preference to
    // whatever the OS happened to be on the day it was set.
    expect(screen.getByTestId("theme")).toHaveTextContent("system");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  /*
   * The theme TRANSITION, which is a different guarantee from the theme.
   *
   * Startup applies the theme twice — the OS preference at first paint, then the stored one a
   * moment later. Animating either fades the whole application in from the wrong colours on
   * every launch, which looks like a bug and happens every single time.
   *
   * So the drill is on the gate, not on the animation: the attribute the CSS keys off must be
   * absent while preferences are in flight, and present once they have landed.
   */
  it("does not animate the theme until the stored preference has arrived", async () => {
    let release: (value: unknown) => void = () => {};
    const pending = new Promise((resolve) => {
      release = resolve;
    });

    installBridge({
      Config: {
        Preferences: () => pending as Promise<{ ok: true; data: unknown }>,
      },
    });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );

    // In flight: the theme is already applied from the OS, and must not be animated.
    await waitFor(() => expect(document.documentElement.dataset.theme).toBeTruthy());
    expect(document.documentElement.dataset.themeTransitions).toBeUndefined();

    release({ ok: true, data: { locale: "en", theme: "dark", availableLocales: ["en"], landing: "/" } });

    await waitFor(() =>
      expect(document.documentElement.dataset.themeTransitions).toBeDefined(),
    );
  });

  it("settles the transition gate even when preferences cannot be read", async () => {
    // Otherwise a shop whose preferences fail to load never gets an animated theme change
    // again — a permanent degradation from a transient failure.
    installBridge({ Config: { Preferences: () => Promise.reject(new Error("disk")) } });

    renderIn(
      <PreferencesProvider>
        <Probe />
      </PreferencesProvider>,
    );

    await waitFor(() =>
      expect(document.documentElement.dataset.themeTransitions).toBeDefined(),
    );
  });
});
