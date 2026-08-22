import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { chartOfAccounts, type Account } from "@/lib/wails";
import { Alert, EmptyState, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * The chart of accounts, read-only (§20.6 tier v1.1).
 *
 * Read-only is the design, not a shortcut: §20.6 exposes accounting over five releases, and
 * editing the chart is v1.3's. Shipping an edit button now would put a control behind a
 * permission nobody has been granted and a workflow nobody has designed.
 */
export function ChartScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const accounts = useQuery({ queryKey: ["accounting", "chart"], queryFn: chartOfAccounts });

  if (accounts.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (accounts.isError) {
    return <Alert tone="danger" title={t("accounting.chart.failed")}>{errorText(accounts.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-4">
      <PageHeader title={t("accounting.chart.title")} description={t("accounting.chart.help")} />

      <Table<Account>
        caption={t("accounting.chart.title")}
        rowKey={(account) => account.id}
        rows={accounts.data}
        empty={<EmptyState title={t("accounting.chart.none")} />}
        columns={[
          {
            key: "code",
            header: t("accounting.chart.code"),
            cell: (account) => (
              // Indented by DEPTH, which the backend sends rather than the frontend deriving
              // from the path — one answer to "how deep is this account", computed where the
              // hierarchy is built.
              <span style={{ paddingInlineStart: `${account.depth * 1.25}rem` }} className="font-mono text-xs">
                {account.code}
              </span>
            ),
          },
          {
            key: "name",
            header: t("accounting.chart.name"),
            cell: (account) => (
              <span className={account.isPostable ? "text-text" : "font-medium text-text"}>
                {/* The chart is translated like everything else: nameKey first, the stored
                    name only as the fallback for a locale nobody has translated yet. */}
                {account.nameKey ? t(account.nameKey) : account.name}
              </span>
            ),
          },
          {
            key: "type",
            header: t("accounting.chart.type"),
            cell: (account) => t(`accounting.type.${account.type}`),
          },
          {
            key: "kind",
            header: t("accounting.chart.postable"),
            cell: (account) =>
              // A heading is not a defect, it is the shape of a chart — so it reads as a
              // description rather than as a missing capability.
              account.isPostable ? t("accounting.chart.leaf") : t("accounting.chart.heading"),
          },
        ]}
      />
    </section>
  );
}
