import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { GateScreen } from "@/app/gates/GateScreen";
import { SetupWizard } from "@/modules/setup/SetupWizard";
import { setupStatus } from "@/lib/wails";

/** The query key for setup status, shared so the wizard can invalidate it after it finishes. */
export const SETUP_STATUS_KEY = ["setup", "status"] as const;

/**
 * Decides between the setup wizard and everything below it.
 *
 * # A gate is a MOUNT BOUNDARY, not a redirect (Step 1.10, D1)
 *
 * `children` is not rendered while the answer is unknown. That is the whole design: React would
 * otherwise mount the shell, fire its queries, and unmount it a tick later — a visible flash,
 * plus a burst of binding calls that all fail because there is no company to answer them.
 *
 * The same discipline §FE.1 asks for, and the same one BootGate applies above: nothing mounts
 * over a database that cannot serve it.
 *
 * # This is not security
 *
 * A fresh install has no user and no data, so there is nothing here to protect. The backend's
 * `setupGuard` (1.9 D2) is what makes `Setup.Apply` unreachable after setup, and it is
 * structural. This component decides what to SHOW.
 */
export function SetupGate({ children }: { children: ReactNode }) {
  const status = useQuery({
    queryKey: SETUP_STATUS_KEY,
    queryFn: setupStatus,
  });

  if (status.isPending) {
    return <GateScreen kind="loading" />;
  }
  if (status.isError) {
    // "Is setup required?" is unanswerable, so neither branch is safe to take: showing the
    // wizard on a configured install would offer to re-create a company that exists, and
    // showing the shell would query a database that may have none. Reporting the failure is
    // the honest third option.
    return <GateScreen kind="error" error={status.error} />;
  }
  if (status.data.required) {
    return <SetupWizard options={status.data.options} />;
  }
  return <>{children}</>;
}
