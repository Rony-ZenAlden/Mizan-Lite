import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { expectConsoleErrors } from "@/test/setup";
import { ErrorBoundary } from "./ErrorBoundary";

function Explodes(): never {
  throw new Error("render failure");
}

describe("ErrorBoundary", () => {
  it("replaces a crashed tree with a translated message in the document's language", () => {
    expectConsoleErrors(); // React and the boundary both log the caught error, by design
    document.documentElement.lang = "ar";
    render(
      <ErrorBoundary>
        <Explodes />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("heading", { name: "حدث خطأ غير متوقع" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "إعادة التحميل" })).toBeInTheDocument();
  });

  it("renders its children when nothing fails", () => {
    render(
      <ErrorBoundary>
        <p>fine</p>
      </ErrorBoundary>,
    );
    expect(screen.getByText("fine")).toBeInTheDocument();
  });
});
