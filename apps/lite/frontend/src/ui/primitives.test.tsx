import { fireEvent, render, screen } from "@testing-library/react";
import { useEffect, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Alert } from "./Alert";
import { Button } from "./Button";
import { Dialog } from "./Dialog";
import { Spinner } from "./Spinner";

describe("primitives", () => {
  it("a button does not submit a form unless asked to", () => {
    render(<Button>go</Button>);
    expect(screen.getByRole("button")).toHaveAttribute("type", "button");
  });

  it("a danger alert interrupts; a success alert does not", () => {
    render(
      <>
        <Alert tone="danger" title="bad" />
        <Alert tone="success" title="good" />
      </>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("bad");
    expect(screen.getByRole("status")).toHaveTextContent("good");
  });

  it("a spinner has an accessible name", () => {
    render(<Spinner label="loading" />);
    expect(screen.getByRole("status")).toHaveTextContent("loading");
  });

  it("a dialog moves focus in once, and a parent re-rendering with a new onClose does not pull it back (0.10.0)", () => {
    const closed = vi.fn();
    function Ticking() {
      const [tick, setTick] = useState(0);
      useEffect(() => {
        if (tick < 3) setTick(tick + 1);
      }, [tick]);
      return (
        <Dialog title="t" onClose={() => closed(tick)}>
          <input aria-label="first" />
          <input aria-label="second" />
          <p>{tick}</p>
        </Dialog>
      );
    }
    const { rerender } = render(<Ticking />);
    expect(screen.getByLabelText("first")).toHaveFocus();
    screen.getByLabelText("second").focus();
    rerender(<Ticking />);
    expect(screen.getByLabelText("second")).toHaveFocus();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(closed).toHaveBeenCalledWith(3);
  });
});
