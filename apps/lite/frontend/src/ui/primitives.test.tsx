import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Alert } from "./Alert";
import { Button } from "./Button";
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
});
