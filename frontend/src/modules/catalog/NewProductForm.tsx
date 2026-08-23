import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { createProduct, productCategories, units } from "@/lib/wails";
import { Alert, Button, Input, Select } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Registering a product (Step 10.10).
 *
 * # Two fields, not eleven
 *
 * The service accepts eleven. This asks for a code and a name, and offers three more that are
 * already filled in. That is not a simplification of an existing form — until 10.10 there was NO
 * form, and the only way to add a product was to write a CSV and import it.
 *
 * Everything hidden has an answer that is right for most shops most of the time: goods rather
 * than a service, the shop's usual unit, and no category. A product that cannot be saved until it
 * is filed is a product somebody keys into a notebook instead.
 */
export function NewProductForm({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [unit, setUnit] = useState("");
  const [category, setCategory] = useState("");
  const [more, setMore] = useState(false);

  const unitList = useQuery({ queryKey: ["catalog", "units"], queryFn: units });
  const categoryList = useQuery({ queryKey: ["catalog", "categories"], queryFn: productCategories });

  const create = useMutation({
    mutationFn: createProduct,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["catalog"] });
      // Cleared, not closed. Somebody adding products is usually adding several, and a form that
      // shuts after each one makes them press New for every item on a delivery note.
      setCode("");
      setName("");
      onCreated();
    },
  });

  const ready = code.trim() !== "" && name.trim() !== "";

  return (
    <form
      className="flex flex-col gap-3 card p-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (ready) {
          create.mutate({ code: code.trim(), name: name.trim(), unit, category, type: "" });
        }
      }}
    >
      <h3 className="text-sm font-medium text-text">{t("catalog.newProduct")}</h3>

      {create.isError && (
        <Alert tone="danger" title={t("catalog.createFailed")}>{errorText(create.error)}</Alert>
      )}
      {create.isSuccess && !create.isError && (
        <Alert tone="info" title={t("catalog.created")}>{t("catalog.createdHelp")}</Alert>
      )}

      <div className="flex flex-col gap-3 sm:flex-row">
        <Input
          label={t("catalog.code")}
          value={code}
          onChange={(event) => setCode(event.target.value)}
          placeholder={t("catalog.codeHint")}
        />
        <Input
          label={t("catalog.name")}
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder={t("catalog.nameHint")}
        />
      </div>

      {/*
       * The rest is behind one press.
       *
       * A unit and a category are right by default for most shops, and asking every time turns a
       * two-field job into a five-field one — which is how a form stops being used.
       */}
      {more && (
        <div className="flex flex-col gap-3 sm:flex-row">
          <Select
            label={t("catalog.unit")}
            value={unit}
            onValueChange={setUnit}
            options={[
              { value: "", label: t("catalog.defaultUnit") },
              ...(unitList.data ?? []).map((u) => ({ value: u.code, label: u.name })),
            ]}
          />
          <Select
            label={t("catalog.category")}
            value={category}
            onValueChange={setCategory}
            options={[
              { value: "", label: t("catalog.noCategory") },
              ...(categoryList.data ?? []).map((c) => ({ value: c.code, label: c.name })),
            ]}
          />
        </div>
      )}

      <div className="flex items-center justify-between gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => setMore((v) => !v)}>
          {more ? t("catalog.fewerOptions") : t("catalog.moreOptions")}
        </Button>
        <Button type="submit" disabled={!ready || create.isPending}>
          {t("catalog.save")}
        </Button>
      </div>
    </form>
  );
}
