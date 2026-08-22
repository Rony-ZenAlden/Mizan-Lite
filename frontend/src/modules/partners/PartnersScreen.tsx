import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { NewPartnerForm } from "./NewPartnerForm";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import {
  PERMISSIONS,
  customer, customers, supplier, suppliers,
  type PartnerDetail as PartnerDetailData, type PartnerRow,
} from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Input, PageHeader, Sheet, Table } from "@/shared/ui";
import { Can } from "@/app/session/Can";
import { useErrorText } from "@/modules/admin/useAdminError";
import { PartnerDetail } from "./PartnerDetail";

/**
 * Customers and suppliers.
 *
 * # Two screens over one table
 *
 * Decision 9 puts both roles in one `partners` table, and the reason shows here: a partner who
 * is both appears in BOTH lists, as one identity with one tax number and one balance. The screens
 * are separate because the permissions are — a salesperson has no business reading a supplier's
 * payment terms — not because the data is.
 */
export function PartnersScreen({ role }: { role: "customer" | "supplier" }) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState("");
  const [adding, setAdding] = useState(false);

  const rows = useQuery({
    queryKey: ["partners", role, search],
    queryFn: () => (role === "customer" ? customers(search) : suppliers(search)),
  });

  const title = role === "customer" ? t("partners.customers") : t("partners.suppliers");

  /*
   * A DRAWER, not a replacement.
   *
   * Until 10.16 picking a partner returned the detail view in place of this whole screen, so the
   * search term was discarded and came back empty on the way out. Someone reconciling a list of
   * suppliers re-typed the search once per supplier.
   */
  const openPartner = rows.data?.find((row) => row.code === selected);

  return (
    <section className="flex flex-col gap-4">
      <Sheet
        open={selected !== ""}
        onOpenChange={(open) => {
          if (!open) setSelected("");
        }}
        title={openPartner ? openPartner.name : ""}
      >
        {selected ? (
          <PartnerDetail
            code={selected}
            role={role}
            load={(code: string): Promise<PartnerDetailData> =>
              role === "customer" ? customer(code) : supplier(code)
            }
            onBack={() => setSelected("")}
            embedded
          />
        ) : null}
      </Sheet>

      <PageHeader
        title={title}
        description={t(`partners.${role}.help`)}
        actions={
          <Can
            permission={
              role === "customer" ? PERMISSIONS.customerManage : PERMISSIONS.supplierManage
            }
          >
            <Button onClick={() => setAdding((open) => !open)}>
              {adding
                ? t("catalog.done")
                : role === "customer"
                  ? t("partners.newCustomer")
                  : t("partners.newSupplier")}
            </Button>
          </Can>
        }
      />

      {adding ? <NewPartnerForm role={role} /> : null}

      <Input
        label={t("partners.search")}
        /* Searched on the SERVER, unlike the catalog: a partner list is unbounded where a
           category tree is not, and the query already matches name, code, and tax number —
           the last of which matters at a counter, where the customer hands over a card with a
           number on it and nothing else the operator can type. */
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />

      {rows.isPending && <p className="text-sm text-text-muted">{t("gate.checking")}</p>}
      {rows.isError && (
        <Alert tone="danger" title={t("partners.failed")}>{errorText(rows.error)}</Alert>
      )}

      {rows.data && (
        <Table<PartnerRow>
          caption={title}
          rowKey={(row) => row.id}
          rows={rows.data}
          empty={<EmptyState title={t("partners.none")} />}
          columns={[
            {
              key: "code",
              header: t("partners.code"),
              cell: (row) => (
                <button
                  type="button"
                  className="font-mono text-xs text-accent underline-offset-2 hover:underline"
                  onClick={() => setSelected(row.code)}
                >
                  {row.code}
                </button>
              ),
            },
            { key: "name", header: t("partners.name"), cell: (row) => row.name },
            {
              key: "roles",
              header: t("partners.roles"),
              /* The case decision 9 exists for, shown plainly: one row, both badges. */
              cell: (row) => (
                <span className="flex gap-1">
                  {row.isCustomer && <Badge tone="info">{t("partners.customer")}</Badge>}
                  {row.isSupplier && <Badge tone="info">{t("partners.supplier")}</Badge>}
                </span>
              ),
            },
            {
              key: "terms",
              header: t("partners.terms"),
              cell: (row) =>
                row.paymentTermsDays === 0
                  ? t("partners.cash")
                  : t("partners.days", { days: String(row.paymentTermsDays) }),
            },
            {
              key: "status",
              header: t("partners.status"),
              cell: (row) => (row.isActive ? t("partners.active") : t("partners.retired")),
            },
          ]}
        />
      )}
    </section>
  );
}
