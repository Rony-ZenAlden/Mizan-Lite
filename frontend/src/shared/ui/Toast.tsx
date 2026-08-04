import * as RadixToast from "@radix-ui/react-toast";
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { cn } from "./cn";

export type ToastTone = "info" | "success" | "danger";

interface ToastMessage {
  id: number;
  title: string;
  description?: string;
  tone: ToastTone;
}

interface ToastApi {
  /** Shows a toast. The caller passes TRANSLATED text — this layer renders, it does not resolve. */
  show: (message: { title: string; description?: string; tone?: ToastTone }) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

/** Access the toast API. Throws when used outside the provider, which is a wiring bug. */
export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (!api) throw new Error("useToast must be used inside <ToastProvider>");
  return api;
}

const TONES: Record<ToastTone, string> = {
  info: "border-border",
  success: "border-success",
  danger: "border-danger",
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [messages, setMessages] = useState<ToastMessage[]>([]);

  const show = useCallback<ToastApi["show"]>((message) => {
    setMessages((current) => [
      ...current,
      { id: Date.now() + current.length, tone: "info", ...message },
    ]);
  }, []);

  const api = useMemo(() => ({ show }), [show]);

  return (
    <ToastContext.Provider value={api}>
      <RadixToast.Provider swipeDirection="right">
        {children}
        {messages.map((message) => (
          <RadixToast.Root
            key={message.id}
            duration={6000}
            onOpenChange={(open) => {
              if (!open) setMessages((current) => current.filter((m) => m.id !== message.id));
            }}
            className={cn(
              "flex flex-col gap-1 rounded border bg-surface p-3 shadow-lg",
              TONES[message.tone],
            )}
          >
            <RadixToast.Title className="text-sm font-medium text-text">
              {message.title}
            </RadixToast.Title>
            {message.description ? (
              <RadixToast.Description className="text-xs text-text-muted">
                {message.description}
              </RadixToast.Description>
            ) : null}
          </RadixToast.Root>
        ))}
        {/* Anchored with logical inset so it sits in the trailing corner in both directions. */}
        <RadixToast.Viewport className="fixed bottom-0 end-0 z-50 flex w-80 max-w-[100vw] flex-col gap-2 p-4" />
      </RadixToast.Provider>
    </ToastContext.Provider>
  );
}
