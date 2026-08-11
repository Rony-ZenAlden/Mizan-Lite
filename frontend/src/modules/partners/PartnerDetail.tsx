import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import type { PartnerAddress, PartnerContact, PartnerDetail as Detail } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * One partner: their terms, their addresses, and the people at them.
 */
export function PartnerDetail(props: {
  code: string;
  role: "customer" | "supplier";
  load: (code: string) => Promise<Detail>;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const detail = useQuery({
    queryKey: ["partners", props.role, "detail", props.code],
    queryFn: () => props.load(props.code),
  });

  if (detail.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (detail.isError) {
    return (
      <section className="flex flex-col gap-4">
        <Button variant="ghost" onClick={props.onBack}>{t("partners.back")}</Button>
        <Alert tone="danger" title={t("partners.failed")}>{errorText(detail.error)}</Alert>
      </section>
    );
  }

  const { partner, addresses, contacts } = detail.data;

  return (
    <section className="flex flex-col gap-4">
      <div>
        <Button variant="ghost" onClick={props.onBack}>{t("partners.back")}</Button>
      </div>

      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{partner.name}</h2>
        <p className="font-mono text-xs text-text-muted">{partner.code}</p>
        <span className="flex gap-1 pt-1">
          {partner.isCustomer && <Badge tone="info">{t("partners.customer")}</Badge>}
          {partner.isSupplier && <Badge tone="info">{t("partners.supplier")}</Badge>}
        </span>
      </header>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm md:grid-cols-4">
        <Field label={t("partners.legalName")} value={partner.legalName} />
        <Field label={t("partners.taxNumber")} value={partner.taxNumber} />
        <Field
          label={t("partners.terms")}
          value={
            partner.paymentTermsDays === 0
              ? t("partners.cash")
              : t("partners.days", { days: String(partner.paymentTermsDays) })
          }
        />
        {partner.isCustomer && (
          <Field
            label={t("partners.creditLimit")}
            /* "0" means NO LIMIT, not "no credit" — the two readings differ by every sale the
               business makes, so the screen says which one it is rather than printing a zero. */
            value={
              partner.creditLimitMinor === "0"
                ? t("partners.noCreditLimit")
                : `${partner.creditLimitMinor} ${partner.currency}`
            }
          />
        )}
        <Field label={t("partners.phone")} value={partner.phone} />
        <Field label={t("partners.email")} value={partner.email} />
      </dl>

      {partner.isTaxExempt && (
        <Alert tone="info" title={t("partners.taxExempt")}>{t("partners.taxExemptHelp")}</Alert>
      )}

      {/* Explaining a locked role rather than offering a toggle that always refuses: a supplier
          who has been billed has a payable in the accounts, and clearing the flag would hide
          them from the supplier list while the balance remains. */}
      {(detail.data.customerRoleLocked || detail.data.supplierRoleLocked) && (
        <Alert tone="info" title={t("partners.roleLocked")}>{t("partners.roleLockedHelp")}</Alert>
      )}

      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium text-text">{t("partners.addresses")}</h3>
        <Table<PartnerAddress>
          caption={t("partners.addresses")}
          rowKey={(address) => address.id}
          rows={addresses}
          empty={<EmptyState title={t("partners.noAddresses")} />}
          columns={[
            {
              key: "label",
              header: t("partners.label"),
              cell: (address) => (
                <span className="flex items-center gap-2">
                  {address.label}
                  {address.isDefault && <Badge tone="neutral">{t("partners.default")}</Badge>}
                </span>
              ),
            },
            {
              key: "type",
              header: t("partners.addressType"),
              cell: (address) => t(`partners.address.${address.addressType}`),
            },
            {
              key: "address",
              header: t("partners.address"),
              cell: (address) =>
                [address.line1, address.line2, address.city, address.region]
                  .filter(Boolean)
                  .join(", "),
            },
          ]}
        />
      </div>

      <div className="flex flex-col gap-2">
        <h3 className="text-sm font-medium text-text">{t("partners.contacts")}</h3>
        <Table<PartnerContact>
          caption={t("partners.contacts")}
          rowKey={(contact) => contact.id}
          rows={contacts}
          empty={<EmptyState title={t("partners.noContacts")} />}
          columns={[
            {
              key: "name",
              header: t("partners.name"),
              cell: (contact) => (
                <span className="flex items-center gap-2">
                  {contact.name}
                  {contact.isPrimary && <Badge tone="neutral">{t("partners.primary")}</Badge>}
                </span>
              ),
            },
            { key: "role", header: t("partners.role"), cell: (contact) => contact.role || "—" },
            { key: "phone", header: t("partners.phone"), cell: (contact) => contact.phone || "—" },
            { key: "email", header: t("partners.email"), cell: (contact) => contact.email || "—" },
          ]}
        />
      </div>
    </section>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-text-muted">{label}</dt>
      <dd className="text-text">{value || "—"}</dd>
    </div>
  );
}
