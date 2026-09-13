import { useEffect, useState, type ReactNode } from "react";
import { useClient } from "@/api/ClientContext";
import type { BootStatus } from "@/api/client";
import { BindingError, CODE_BRIDGE_UNAVAILABLE } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import type { MessageKey } from "@/i18n/messages";
import { Spinner } from "@/ui/Spinner";

/** How often the boot screen asks how far startup has got. */
export const BOOT_POLL_MS = 150;

type View =
  | { kind: "starting"; status?: BootStatus }
  | { kind: "ready" }
  | { kind: "failed"; status: BootStatus }
  | { kind: "bridge-unavailable" };

const PHASE_KEYS: Record<string, MessageKey> = {
  checking: "boot.phase.checking",
  backup: "boot.phase.backup",
  migrating: "boot.phase.migrating",
  restoring: "boot.phase.restoring",
  done: "boot.phase.done",
};

/**
 * Renders its children only once the backend reports "ready".
 *
 * Until then it shows progress; if startup failed it shows why and where the backup is; and if there
 * is no backend at all it says so — it never renders the application over a graph that does not exist.
 */
export function Boot({ children }: { children: ReactNode }) {
  const client = useClient();
  const [view, setView] = useState<View>({ kind: "starting" });

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const poll = async () => {
      try {
        const status = await client.app.bootStatus();
        if (cancelled) return;
        if (status.state === "ready") {
          setView({ kind: "ready" });
          return;
        }
        if (status.state === "failed") {
          setView({ kind: "failed", status });
          return;
        }
        setView({ kind: "starting", status });
      } catch (error) {
        if (cancelled) return;
        if (error instanceof BindingError && error.code === CODE_BRIDGE_UNAVAILABLE) {
          setView({ kind: "bridge-unavailable" });
          return;
        }
        // A transient IPC failure while the backend is busy migrating: keep asking.
      }
      timer = setTimeout(poll, BOOT_POLL_MS);
    };

    void poll();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [client]);

  switch (view.kind) {
    case "ready":
      return <>{children}</>;
    case "bridge-unavailable":
      return <BridgeUnavailable />;
    case "failed":
      return <BootFailure status={view.status} />;
    default:
      return <BootProgress status={view.status} />;
  }
}

function BootProgress({ status }: { status?: BootStatus }) {
  const { t } = useLocale();
  const phaseKey = (status?.phase && PHASE_KEYS[status.phase]) || "boot.phase.starting";
  return (
    <main className="flex h-full flex-col items-center justify-center gap-3 p-8">
      <h1 className="text-2xl font-semibold">{t("app.name")}</h1>
      <Spinner label={t(phaseKey)} />
      {status && status.total > 0 ? (
        <p className="text-sm text-text-muted">
          {t("boot.progress", { current: String(status.current), total: String(status.total) })}
        </p>
      ) : null}
    </main>
  );
}

function BootFailure({ status }: { status: BootStatus }) {
  const { t, tDynamic } = useLocale();
  return (
    <main className="flex h-full items-center justify-center p-8">
      <section role="alert" className="max-w-xl space-y-4 rounded-md border border-danger/40 bg-surface-raised p-6">
        <h1 className="text-xl font-semibold text-danger">{t("boot.failed.title")}</h1>
        {status.error ? <p>{tDynamic(status.error.messageKey, status.error.params)}</p> : null}
        {status.reason ? (
          <p className="text-sm">
            <span className="font-medium">{t("boot.failed.reason")}: </span>
            {tDynamic(status.reason.messageKey, status.reason.params)}
          </p>
        ) : null}
        {status.backupPath ? (
          <div className="text-sm">
            <p className="font-medium">{t("boot.failed.backup")}</p>
            {/* A file path is left-to-right text inside a right-to-left sentence; isolate it. */}
            <bdi dir="ltr" className="block break-all rounded bg-surface p-2 font-mono text-xs">
              {status.backupPath}
            </bdi>
          </div>
        ) : null}
        <p className="text-sm text-text-muted">{t("boot.failed.next")}</p>
      </section>
    </main>
  );
}

function BridgeUnavailable() {
  const { t } = useLocale();
  return (
    <main className="flex h-full items-center justify-center p-8">
      <section role="alert" className="max-w-xl space-y-3 text-center">
        <h1 className="text-xl font-semibold">{t("bridge.unavailable.title")}</h1>
        <p className="text-text-muted">{t("bridge.unavailable.body")}</p>
      </section>
    </main>
  );
}
