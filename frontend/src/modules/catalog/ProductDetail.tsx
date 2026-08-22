import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { product, type ProductAttribute, type Variant } from "@/lib/wails";
import { Alert, Button, EmptyState, PageHeader, Table } from "@/shared/ui";
import { Can } from "@/app/session/Can";
import { PERMISSIONS } from "@/lib/wails";
import { EditProductForm } from "./EditProductForm";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * One product: its units, its variants, and its attributes.
 *
 * # The §A.1 rule, made invisible
 *
 * Every product has at least one variant — a bag of cement included. This screen shows the
 * variants section ONLY when there is more than the default, because a shop that sells cement
 * should never encounter the word "variant". `isSimple` comes from the backend rather than being
 * derived here, so the rule has one home.
 */
export function ProductDetail({
  code,
  onBack,
  embedded = false,
}: {
  code: string;
  onBack: () => void;
  /**
   * Rendered inside a drawer, which supplies the title and the way out.
   *
   * The flag suppresses this component's OWN chrome rather than the drawer hiding it, because a
   * back button and a close button side by side are two controls that do the same thing — and a
   * user who tries the wrong one learns the screen is unpredictable.
   */
  embedded?: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const { t } = useTranslation();
  const errorText = useErrorText();

  const detail = useQuery({
    queryKey: ["catalog", "product", code],
    queryFn: () => product(code),
  });

  if (detail.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (detail.isError) {
    return (
      <section className="flex flex-col gap-4">
        {embedded ? null : (
          <Button variant="ghost" onClick={onBack}>{t("catalog.back")}</Button>
        )}
        <Alert tone="danger" title={t("catalog.failed")}>{errorText(detail.error)}</Alert>
      </section>
    );
  }

  const { product: row, variants, attributes, isSimple } = detail.data;
  const name = row.nameKey ? t(row.nameKey) : row.name;

  return (
    <section className="flex flex-col gap-4">
      {embedded ? (
        <div className="flex items-center justify-between gap-2">
          <p className="font-mono text-xs text-text-muted">{row.code}</p>
          <Can permission={PERMISSIONS.catalogManage}>
            <Button variant="ghost" onClick={() => setEditing((open) => !open)}>
              {editing ? t("catalog.done") : t("catalog.edit")}
            </Button>
          </Can>
        </div>
      ) : (
        <>
          <div>
            <Button variant="ghost" onClick={onBack}>{t("catalog.back")}</Button>
          </div>

          <PageHeader
            title={name}
            actions={
              <Can permission={PERMISSIONS.catalogManage}>
                <Button variant="ghost" onClick={() => setEditing((open) => !open)}>
                  {editing ? t("catalog.done") : t("catalog.edit")}
                </Button>
              </Can>
            }
          />
          <p className="-mt-4 font-mono text-xs text-text-muted">{row.code}</p>
        </>
      )}

      {editing ? (
        <EditProductForm
          code={row.code}
          initialName={row.name}
          initialDescription={row.description ?? ""}
          initialCategory={row.categoryCode ?? ""}
          onSaved={() => setEditing(false)}
        />
      ) : null}

      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm md:grid-cols-4">
        <Field label={t("catalog.stockUnit")} value={row.stockUnit} />
        <Field label={t("catalog.salesUnit")} value={detail.data.salesUnit} />
        <Field label={t("catalog.purchaseUnit")} value={detail.data.purchaseUnit} />
        <Field label={t("catalog.tracking")} value={t(`catalog.tracking.${row.tracking}`)} />
      </dl>

      {/* Explaining the lock rather than offering a control that always refuses: changing the
          stock unit after stock has moved would restate every historical quantity by the factor
          between the two units, silently. */}
      {detail.data.stockUnitLocked && (
        <Alert tone="info" title={t("catalog.stockUnitLocked")}>
          {t("catalog.stockUnitLockedHelp")}
        </Alert>
      )}

      {!isSimple && (
        <div className="flex flex-col gap-2">
          <h3 className="text-sm font-medium text-text">{t("catalog.variants")}</h3>
          <Table<Variant>
            caption={t("catalog.variants")}
            rowKey={(variant) => variant.id}
            rows={variants.filter((variant) => !variant.isDefault || variant.combination !== "")}
            empty={<EmptyState title={t("catalog.noVariants")} />}
            columns={[
              {
                key: "sku",
                header: t("catalog.sku"),
                cell: (variant) => <span className="font-mono text-xs">{variant.sku}</span>,
              },
              {
                key: "combination",
                header: t("catalog.combination"),
                /* 'COLOUR:RED|SIZE:L' rendered as "RED · L". The canonical string is the
                   backend's matching key, not something a person should have to read. */
                cell: (variant) =>
                  variant.combination
                    .split("|")
                    .map((pair) => pair.split(":")[1] ?? pair)
                    .join(" · "),
              },
              {
                key: "status",
                header: t("catalog.status"),
                cell: (variant) =>
                  variant.isActive ? t("catalog.active") : t("catalog.retired"),
              },
            ]}
          />
        </div>
      )}

      {attributes.length > 0 && (
        <div className="flex flex-col gap-2">
          <h3 className="text-sm font-medium text-text">{t("catalog.attributes")}</h3>
          <Table<ProductAttribute>
            caption={t("catalog.attributes")}
            rowKey={(attribute) => attribute.code}
            rows={attributes}
            empty={<EmptyState title={t("catalog.noAttributes")} />}
            columns={[
              {
                key: "name",
                header: t("catalog.attribute"),
                cell: (attribute) =>
                  attribute.nameKey ? t(attribute.nameKey) : attribute.name,
              },
              {
                key: "kind",
                header: t("catalog.attributeKind"),
                /* §A.2 on screen: the same attribute is a variant dimension for one product and
                   a specification for another, so the label belongs to the LINK. */
                cell: (attribute) =>
                  attribute.isVariantDefining
                    ? t("catalog.variantDefining")
                    : t("catalog.specification"),
              },
              {
                key: "values",
                header: t("catalog.values"),
                cell: (attribute) =>
                  attribute.values
                    .map((value) => (value.nameKey ? t(value.nameKey) : value.name))
                    .join(", "),
              },
            ]}
          />
        </div>
      )}
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
