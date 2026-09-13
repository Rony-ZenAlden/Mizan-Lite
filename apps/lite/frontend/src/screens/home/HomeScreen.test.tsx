import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
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
});
