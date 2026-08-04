import { Component, type ErrorInfo, type ReactNode } from "react";
import { isBindingError } from "@/lib/wails";
import { documentLocale, translate } from "@/i18n/messages";
import { Alert, Button } from "@/shared/ui";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

/**
 * The outermost provider.
 *
 * A BindingError thrown from any screen renders a translated error state instead of a blank
 * window — the white-screen failure that makes a desktop application feel broken beyond
 * recovery, and the reason `call` throws rather than returning a union (§3.1).
 *
 * It reads the locale from <html lang> rather than from the preferences context, because the
 * thing that failed may BE that context. An error boundary that depends on the tree it is
 * guarding fails exactly when it is needed.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Developer detail goes to the console, never to the screen (§22.2).
    console.error("unhandled error in the shell", error, info.componentStack);
  }

  private reset = () => this.setState({ error: null });

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;

    const locale = documentLocale();
    // A BindingError renders from its code; anything else is a genuine frontend bug, whose
    // Go-side equivalent would be errs.Internal — a generic message, detail in the log.
    const key = isBindingError(error) ? error.messageKey : "app.unknown_error";
    const params = isBindingError(error) ? error.params : undefined;

    return (
      <div className="flex h-full items-center justify-center p-8">
        <div className="flex w-full max-w-md flex-col gap-4">
          <Alert tone="danger" title={translate(locale, "app.unknown_error")}>
            {translate(locale, key, params)}
          </Alert>
          <Button variant="secondary" onClick={this.reset}>
            {translate(locale, "action.retry")}
          </Button>
        </div>
      </div>
    );
  }
}
