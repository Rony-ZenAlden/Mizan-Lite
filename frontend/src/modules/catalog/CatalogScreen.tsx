import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { productCategories, products, type Category, type ProductRow } from "@/lib/wails";
import { Alert, EmptyState, Input, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { ProductDetail } from "./ProductDetail";
import { NewProductForm } from "./NewProductForm";
import { Can } from "@/app/session/Can";
import { Button } from "@/shared/ui";
import { PERMISSIONS } from "@/lib/wails";

/**
 * The catalog browse screen: the category tree beside the products in it.
 *
 * Read-only, like the chart of accounts (§20.6's reasoning applies here too): Phase 3 builds the
 * master data and the screens that read it, and a create form belongs with the release that has
 * designed the workflow around it.
 */
export function CatalogScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [category, setCategory] = useState("");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState("");
  const [adding, setAdding] = useState(false);

  const categories = useQuery({ queryKey: ["catalog", "categories"], queryFn: productCategories });
  const rows = useQuery({ queryKey: ["catalog", "products"], queryFn: products });

  if (categories.isPending || rows.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (categories.isError || rows.isError) {
    return (
      <Alert tone="danger" title={t("catalog.failed")}>
        {errorText(categories.error ?? rows.error)}
      </Alert>
    );
  }

  // Filtering here rather than in a query: the whole catalog is already loaded for the list, and
  // a round trip per keystroke would be slower than the filter it replaces.
  const term = search.trim().toLowerCase();
  const visible = rows.data.filter((row) => {
    if (category && row.categoryCode !== category) return false;
    if (!term) return true;
    return (
      row.code.toLowerCase().includes(term) || row.name.toLowerCase().includes(term)
    );
  });

  if (selected) {
    return <ProductDetail code={selected} onBack={() => setSelected("")} />;
  }

  return (
    <section className="flex flex-col gap-4">
      <PageHeader
        title={t("catalog.title")}
        description={t("catalog.help")}
        actions={
          <>
            {/* Behind the permission that also guards the importer: they add products by two routes,
            and a separate grant for one would protect nothing. */}
        <Can permission={PERMISSIONS.catalogManage}>
          <Button onClick={() => setAdding((v) => !v)}>
            {adding ? t("catalog.done") : t("catalog.newProduct")}
          </Button>
        </Can>
          </>
        }
      />

      {adding && <NewProductForm onCreated={() => undefined} />}

      <div className="flex flex-col gap-4 md:flex-row">
        <nav aria-label={t("catalog.categories")} className="md:w-56 md:shrink-0">
          <ul className="flex flex-col gap-1">
            <li>
              <CategoryButton
                label={t("catalog.allCategories")}
                depth={0}
                selected={category === ""}
                onSelect={() => setCategory("")}
              />
            </li>
            {categories.data.map((node: Category) => (
              <li key={node.id}>
                <CategoryButton
                  /* Translated like everything else: nameKey first, the stored name only as the
                     fallback for a locale nobody has translated yet (§22.4). */
                  label={node.nameKey ? t(node.nameKey) : node.name}
                  /* Indented by DEPTH, which the backend sends rather than the screen deriving
                     it from the path — one answer to "how deep is this". */
                  depth={node.depth}
                  selected={category === node.code}
                  onSelect={() => setCategory(node.code)}
                />
              </li>
            ))}
          </ul>
        </nav>

        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <Input
            label={t("catalog.search")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />

          <Table<ProductRow>
            caption={t("catalog.products")}
            rowKey={(row) => row.id}
            rows={visible}
            empty={<EmptyState title={t("catalog.noProducts")} />}
            columns={[
              {
                key: "code",
                header: t("catalog.code"),
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
              {
                key: "name",
                header: t("catalog.name"),
                cell: (row) => (row.nameKey ? t(row.nameKey) : row.name),
              },
              { key: "unit", header: t("catalog.stockUnit"), cell: (row) => row.stockUnit },
              {
                key: "variants",
                header: t("catalog.variants"),
                /* A simple product shows a dash, not "1". §A.1 gives every product a default
                   variant, and printing "1" here would expose a mechanism designed to be
                   invisible to the shop selling bags of cement. */
                cell: (row) => (row.variantCount > 1 ? String(row.variantCount) : "—"),
              },
              {
                key: "status",
                header: t("catalog.status"),
                cell: (row) => (row.isActive ? t("catalog.active") : t("catalog.retired")),
              },
            ]}
          />
        </div>
      </div>
    </section>
  );
}

function CategoryButton(props: {
  label: string;
  depth: number;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-current={props.selected ? "true" : undefined}
      onClick={props.onSelect}
      style={{ paddingInlineStart: `${0.5 + props.depth * 0.75}rem` }}
      className={`w-full rounded px-2 py-1 text-start text-sm ${
        props.selected ? "bg-surface-raised font-medium text-text" : "text-text-muted hover:text-text"
      }`}
    >
      {props.label}
    </button>
  );
}
