import { useEffect, useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { health, type HealthInfo } from "@/lib/wails";

/**
 * The health screen — the envelope's first consumer, now reading through the wrapper.
 *
 * Moved out of AppShell in Step 1.11: the shell became a router, and a router that also holds
 * a screen is a file two people edit for unrelated reasons.
 */
export function SystemPanel() {
  const { t } = useTranslation();
  const [info, setInfo] = useState<HealthInfo | null>(null);

  useEffect(() => {
    health()
      .then(setInfo)
      .catch(() => setInfo(null));
  }, []);

  return (
    <dl className="grid max-w-md grid-cols-2 gap-2 text-sm">
      <Row label={t("shell.version")} value={info?.version} />
      <Row label={t("shell.platform")} value={info?.platform} />
      <Row label={t("shell.backend")} value={info?.goVersion} />
      <Row label={t("shell.commit")} value={info?.commit} />
    </dl>
  );
}

function Row({ label, value }: { label: string; value?: string }) {
  return (
    <>
      <dt className="text-text-muted">{label}</dt>
      <dd className="font-mono text-text">{value ?? "—"}</dd>
    </>
  );
}
