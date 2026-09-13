import type { ReactNode } from "react";

type Tone = "danger" | "success";

const TONES: Record<Tone, string> = {
  danger: "border-danger/40 bg-danger-subtle text-danger",
  success: "border-success/40 bg-success-subtle text-success",
};

/**
 * A message block. A danger alert is announced immediately (role="alert"); anything else politely
 * (role="status"), so a screen reader does not interrupt a cashier for good news.
 */
export function Alert({ tone, title, children }: { tone: Tone; title: string; children?: ReactNode }) {
  return (
    <div role={tone === "danger" ? "alert" : "status"} className={`rounded-md border p-4 ${TONES[tone]}`}>
      <p className="font-semibold">{title}</p>
      {children ? <div className="mt-1 text-sm">{children}</div> : null}
    </div>
  );
}
