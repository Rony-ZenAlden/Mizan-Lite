import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { auditEntries, payloadHidden, type AuditEntry } from "@/lib/wails";
import { Alert, EmptyState, Input, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "./useAdminError";

/**
 * The audit trail.
 *
 * # Three states, not two (1.11 D7)
 *
 * A payload field is ABSENT — not blank — for a caller without `audit.entry.view_payload`
 * (1.6). That gives three distinct facts, and the screen must render three:
 *
 *   withheld   the key is missing        you may not see this
 *   empty      the key is present, ""    there was no before-state (a creation)
 *   present    the key holds JSON        the payload
 *
 * Collapsing the first two is the failure 1.6 exists to prevent: a blank cell cannot be told
 * from a forbidden one, so nobody ever asks for the permission they are missing.
 */
export function AuditScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const [entityType, setEntityType] = useState("");

  const entries = useQuery({
    queryKey: ["audit", "entries", entityType],
    queryFn: () => auditEntries({ entityType, limit: 200 }),
  });

  if (entries.isError) {
    return <Alert tone="danger" title={t("admin.audit.failed")}>{errorText(entries.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-4">
      <PageHeader
        title={t("admin.audit.title")}
        actions={
          <>
            <Input
          label={t("admin.audit.filterEntity")}
          value={entityType}
          onChange={(event) => setEntityType(event.target.value)}
          hint={t("admin.audit.filterEntity.hint")}
          className="w-56"
        />
          </>
        }
      />

      <Table<AuditEntry>
        caption={t("admin.audit.title")}
        rowKey={(entry) => entry.id}
        rows={entries.data ?? []}
        empty={<EmptyState title={t("admin.audit.none")} />}
        columns={[
          { key: "when", header: t("admin.audit.when"), cell: (entry) => entry.occurredAt },
          {
            key: "who",
            header: t("admin.audit.who"),
            cell: (entry) =>
              // The snapshot, so a five-year-old entry stays readable after the user is renamed
              // or deactivated (§15.1). An entry with no actor was the system's doing.
              entry.actorName || t("admin.audit.system"),
          },
          { key: "action", header: t("admin.audit.action"), cell: (entry) => t(entry.action) },
          {
            key: "entity",
            header: t("admin.audit.entity"),
            cell: (entry) => entry.entityLabel || entry.entityType,
          },
          {
            key: "payload",
            header: t("admin.audit.payload"),
            cell: (entry) => <Payload entry={entry} />,
          },
        ]}
      />
    </section>
  );
}

function Payload({ entry }: { entry: AuditEntry }) {
  const { t } = useTranslation();

  if (payloadHidden(entry)) {
    return (
      <span className="text-xs italic text-text-muted" title={t("admin.audit.withheld.help")}>
        {t("admin.audit.withheld")}
      </span>
    );
  }
  const body = entry.afterJson || entry.beforeJson;
  if (!body) {
    // Present but empty: a change whose value is the fact, not the content — a password change
    // carries no payload by design (1.7).
    return <span className="text-xs text-text-muted">—</span>;
  }
  return (
    <code className="block max-w-xs truncate text-xs text-text-muted" title={body}>
      {body}
    </code>
  );
}
