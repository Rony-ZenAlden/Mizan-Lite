import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { productCategories, updateProduct } from "@/lib/wails";
import { Alert, Button, Card, Input, Select } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Editing a product (Step 10.15).
 *
 * # Only three fields, and the absences are the design
 *
 * The CODE is shown and not editable. It is what barcodes, imports and integrations match on, and
 * renaming it after anything refers to it is a deletion and a creation wearing one name. The
 * binding cannot express a code change at all, so no screen can make one by mistake.
 *
 * The UNIT, the tracking mode and the type are not here either. Each restates history if changed
 * after stock has moved, and the domain already refuses one of them by name. A general edit form
 * that quietly included them would hide that rule inside a save button.
 *
 * What IS here can be changed at any time, including on a product that has traded for years —
 * because §9.3 means every document snapshots the name it was sold under, so renaming changes
 * what the product is called today and rewrites nothing.
 */
export function EditProductForm({
  code,
  initialName,
  initialDescription,
  initialCategory,
  onSaved,
}: {
  code: string;
  initialName: string;
  initialDescription: string;
  initialCategory: string;
  onSaved?: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [name, setName] = useState(initialName);
  const [description, setDescription] = useState(initialDescription);
  const [category, setCategory] = useState(initialCategory);

  const categories = useQuery({
    queryKey: ["catalog", "categories"],
    queryFn: productCategories,
  });

  const save = useMutation({
    mutationFn: () => updateProduct({ code, name: name.trim(), description, category }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["catalog"] });
      onSaved?.();
    },
  });

  const changed =
    name.trim() !== initialName ||
    description !== initialDescription ||
    category !== initialCategory;

  return (
    <Card title={t("catalog.edit")} description={t("catalog.editHelp")}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          if (changed && name.trim() !== "") save.mutate();
        }}
      >
        {save.isError && (
          <Alert tone="danger" title={t("catalog.editFailed")}>{errorText(save.error)}</Alert>
        )}
        {save.isSuccess && !changed && (
          <Alert tone="info" title={t("catalog.saved")}>{t("catalog.savedHelp")}</Alert>
        )}

        {/*
         * The code, shown and disabled rather than hidden. A person editing a product needs to
         * know which one they have open, and a field they can see but not change teaches that it
         * is fixed — where an absent one just leaves them wondering where it went.
         */}
        <Input label={t("catalog.code")} value={code} disabled readOnly />

        <Input
          label={t("catalog.name")}
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
        <Input
          label={t("catalog.description")}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          hint={t("catalog.descriptionHint")}
        />
        <Select
          label={t("catalog.category")}
          value={category}
          onValueChange={setCategory}
          options={[
            { value: "", label: t("catalog.noCategory") },
            ...(categories.data ?? []).map((c) => ({ value: c.code, label: c.name })),
          ]}
        />

        <div className="flex justify-end">
          {/* Disabled until something changed: a save that writes the same values still bumps the
              row version and leaves an audit entry saying nothing happened. */}
          <Button type="submit" disabled={!changed || name.trim() === "" || save.isPending}>
            {t("catalog.saveChanges")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
