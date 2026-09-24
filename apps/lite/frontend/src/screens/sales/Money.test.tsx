import { act, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "@/api/testing";
import { unsigned } from "@/screens/customers/Balances";
import { Money, isZero } from "./Money";

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

describe("reading a dual figure as text (0.10.0)", () => {
  it("knows a dual zero is zero, and a dual figure is not", () => {
    expect(isZero("0 (0)")).toBe(true);
    expect(isZero("-0.00 (-0)")).toBe(true);
    expect(isZero("0.5 (50)")).toBe(false);
    expect(isZero("0")).toBe(true);
  });

  it("takes the sign off both readings", () => {
    expect(unsigned("-150 (-15000)")).toBe("150 (15000)");
    expect(unsigned("-10.00")).toBe("10.00");
    expect(unsigned("150 (15000)")).toBe("150 (15000)");
  });
});
