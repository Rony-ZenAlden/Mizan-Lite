import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { CODE_OWNER_REQUIRED, OwnerCancelled, useOwner } from "./OwnerProvider";

const required = () => new BindingError({ code: CODE_OWNER_REQUIRED, messageKey: CODE_OWNER_REQUIRED });

/** A button that runs `act` through withOwner and prints what happened. */
function Harness({ act }: { act: () => Promise<string> }) {
  const { withOwner } = useOwner();
  const [outcome, setOutcome] = useState("");
  return (
    <>
      <button
        type="button"
        onClick={() =>
          withOwner(act)
            .then((v) => setOutcome(`ok:${v}`))
            .catch((e) => setOutcome(e instanceof OwnerCancelled ? "cancelled" : `error:${(e as BindingError).code}`))
        }
      >
        run
      </button>
      <output>{outcome}</output>
    </>
  );
}

async function typePin(pin: string) {
  await userEvent.type(await screen.findByLabelText("Owner PIN"), pin);
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
}

describe("withOwner", () => {
  it("runs an act that needs nothing, without asking", async () => {
    const act = vi.fn(async () => "done");
    renderWithProviders(<Harness act={act} />, { locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    expect(await screen.findByText("ok:done")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("asks for the PIN when Go refuses, then retries the act once", async () => {
    let calls = 0;
    const act = vi.fn(async () => {
      calls += 1;
      if (calls === 1) throw required();
      return "priced";
    });
    const elevate = vi.fn(async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 120 }));
    renderWithProviders(<Harness act={act} />, { client: fakeClient({ owner: { elevate } }), locale: "en" });

    await userEvent.click(screen.getByRole("button", { name: "run" }));
    expect(await screen.findByRole("dialog", { name: "Owner PIN required" })).toBeInTheDocument();
    await typePin("246813");

    expect(await screen.findByText("ok:priced")).toBeInTheDocument();
    expect(elevate).toHaveBeenCalledWith("246813");
    expect(act).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("never loops: a second refusal is shown, not answered with another prompt", async () => {
    const act = vi.fn(async (): Promise<string> => {
      throw required();
    });
    renderWithProviders(<Harness act={act} />, { locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    await typePin("246813");

    expect(await screen.findByText(`error:${CODE_OWNER_REQUIRED}`)).toBeInTheDocument();
    expect(act).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("closing the dialog cancels quietly and does not run the act again", async () => {
    const act = vi.fn(async (): Promise<string> => {
      throw required();
    });
    renderWithProviders(<Harness act={act} />, { locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(await screen.findByText("cancelled")).toBeInTheDocument();
    expect(act).toHaveBeenCalledTimes(1);
  });

  it("does not ask for the PIN for any other failure", async () => {
    const act = vi.fn(async (): Promise<string> => {
      throw new BindingError({ code: "lite.catalog.not_found", messageKey: "lite.catalog.not_found" });
    });
    renderWithProviders(<Harness act={act} />, { locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    expect(await screen.findByText("error:lite.catalog.not_found")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});

describe("the PIN dialog", () => {
  it("keeps the dialog open on a wrong PIN and says how many attempts are left", async () => {
    const elevate = vi.fn(async () => {
      throw new BindingError({ code: "lite.owner.wrong_pin", messageKey: "lite.owner.wrong_pin", params: { remaining: "3" } });
    });
    const act = vi.fn(async (): Promise<string> => {
      throw required();
    });
    renderWithProviders(<Harness act={act} />, { client: fakeClient({ owner: { elevate } }), locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    await typePin("739251");

    expect(await screen.findByRole("alert")).toHaveTextContent("Wrong PIN. Attempts left before a wait: 3.");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByLabelText("Owner PIN")).toHaveValue("");
  });

  it("shows the lockout wait", async () => {
    const elevate = vi.fn(async () => {
      throw new BindingError({ code: "lite.owner.locked", messageKey: "lite.owner.locked", params: { seconds: "30" } });
    });
    const act = vi.fn(async (): Promise<string> => {
      throw required();
    });
    renderWithProviders(<Harness act={act} />, { client: fakeClient({ owner: { elevate } }), locale: "ar" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    await userEvent.type(await screen.findByLabelText("رمز المالك"), "739251");
    await userEvent.click(screen.getByRole("button", { name: "تأكيد" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("حاول بعد 30 ثانية");
  });

  it("recovers with the code, shows the NEW code until it is written down, then asks for the new PIN", async () => {
    const recover = vi.fn(async () => ({ recoveryCode: "WXYZ-2345-6789-ABCD" }));
    const act = vi.fn(async (): Promise<string> => {
      throw required();
    });
    renderWithProviders(<Harness act={act} />, { client: fakeClient({ owner: { recover } }), locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "run" }));
    await userEvent.click(await screen.findByRole("button", { name: "Forgot the PIN?" }));

    await userEvent.type(screen.getByLabelText("Recovery code"), "abcd-efgh-jkmn-pqrs");
    await userEvent.type(screen.getByLabelText("New owner PIN"), "581937");
    await userEvent.type(screen.getByLabelText("New owner PIN again"), "581930");
    await userEvent.click(screen.getByRole("button", { name: "Recover" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("The two PINs do not match.");
    expect(recover).not.toHaveBeenCalled();

    await userEvent.clear(screen.getByLabelText("New owner PIN again"));
    await userEvent.type(screen.getByLabelText("New owner PIN again"), "581937");
    await userEvent.click(screen.getByRole("button", { name: "Recover" }));

    expect(await screen.findByText("WXYZ-2345-6789-ABCD")).toBeInTheDocument();
    expect(recover).toHaveBeenCalledWith({ recoveryCode: "abcd-efgh-jkmn-pqrs", newPin: "581937" });
    const close = screen.getByRole("button", { name: "Close" });
    expect(close).toBeDisabled();
    await userEvent.click(screen.getByLabelText("I have written the code down"));
    await userEvent.click(close);

    expect(await screen.findByRole("dialog", { name: "Owner PIN required" })).toBeInTheDocument();
  });
});

describe("owner status", () => {
  it("counts down locally, second by second, and re-reads Go when owner mode runs out", async () => {
    // The clock is driven by hand and every tick is inside act, so no update can land between assertions.
    vi.useFakeTimers();
    try {
      let reads = 0;
      const status = vi.fn(async () => {
        reads += 1;
        return { setUp: true, lockedSeconds: 0, elevatedSeconds: reads === 1 ? 2 : 0 };
      });
      function Show() {
        const { status: s } = useOwner();
        return <output>{`seconds:${s.elevatedSeconds}`}</output>;
      }
      await act(async () => {
        renderWithProviders(<Show />, { client: fakeClient({ owner: { status } }), locale: "en" });
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(screen.getByText("seconds:2")).toBeInTheDocument();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
      expect(screen.getByText("seconds:1")).toBeInTheDocument();
      expect(status).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
      expect(screen.getByText("seconds:0")).toBeInTheDocument();
      expect(status).toHaveBeenCalledTimes(2); // re-read exactly once when it ran out — not once per render
    } finally {
      vi.useRealTimers();
    }
  });
});
