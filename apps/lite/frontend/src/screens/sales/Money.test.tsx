import { act, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "@/api/testing";
import { Money } from "./Money";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

describe("Money — both readings at once (L10, 2026-09-17)", () => {
  it("shows the new figure as the amount and the old one small beside it", async () => {
    renderWithProviders(<Money value="150 (15000)" currency="SYP" />, { locale: "en" });
    await settle();
    const shown = screen.getByTestId("money-dual");
    // The new figure is the amount; the old one is there to be recognised.
    expect(shown).toHaveTextContent("150 SYP");
    expect(shown).toHaveTextContent("(15,000)");
    // Both are grouped as figures, and nothing a screen cannot draw survives into the text.
    expect(shown.textContent).not.toMatch(/\p{C}/u);
  });

  it("shows one figure when the shop reads one currency", async () => {
    renderWithProviders(<Money value="15000" currency="SYP" />, { locale: "en" });
    await settle();
    expect(screen.queryByTestId("money-dual")).not.toBeInTheDocument();
    expect(screen.getByText(/15,000/)).toBeInTheDocument();
  });
});
