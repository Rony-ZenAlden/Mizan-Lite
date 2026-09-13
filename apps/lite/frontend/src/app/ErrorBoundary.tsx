import { Component, type ErrorInfo, type ReactNode } from "react";
import { documentLocale, translate } from "@/i18n/messages";

/**
 * The last line of defence against a render error.
 *
 * It reads the language from <html lang> rather than from the locale context, because the error it
 * catches may be the context's own.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("render error", error, info.componentStack);
  }

  render() {
    if (!this.state.failed) return this.props.children;
    const locale = documentLocale();
    return (
      <main className="flex h-full items-center justify-center p-8">
        <div role="alert" className="max-w-md space-y-3 text-center">
          <h1 className="text-xl font-semibold">{translate(locale, "error.boundary.title")}</h1>
          <p className="text-text-muted">{translate(locale, "error.boundary.body")}</p>
          <button
            type="button"
            className="rounded-md bg-primary px-4 py-2 text-primary-fg"
            onClick={() => window.location.reload()}
          >
            {translate(locale, "action.reload")}
          </button>
        </div>
      </main>
    );
  }
}
