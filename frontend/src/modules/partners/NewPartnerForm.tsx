import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { createCustomer, createSupplier, type NewPartner } from "@/lib/wails";
import { Alert, Button, Card, Input } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

const EMPTY: NewPartner = {
  code: "", name: "", isCustomer: false, isSupplier: false,
  phone: "", email: "", taxNumber: "", paymentTermsDays: 0, creditLimitMinor: "0",
};

/**
 * Registering a customer or a supplier (Step 10.13).
 *
 * # The phone is on the first screen, and that is a decision about the room
 *
 * It is how a shop finds a returning customer — faster and more reliably than by name, in a
 * market where four customers share one. A form that hides it behind "more details" is a form
 * whose records cannot be searched the way people actually search them.
 *
 * Credit terms are NOT here. They matter only when selling on account, most partners are not,
 * and asking everybody a question that applies to a few is how a two-field job becomes a
 * six-field one.
 */
export function NewPartnerForm({ role }: { role: "customer" | "supplier" }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [form, setForm] = useState<NewPartner>(EMPTY);

  const create = useMutation({
    mutationFn: (input: NewPartner) =>
      role === "customer" ? createCustomer(input) : createSupplier(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["partners"] });
      // Cleared rather than closed: somebody entering a supplier list is entering several.
      setForm(EMPTY);
    },
  });

  const ready = form.code.trim() !== "" && form.name.trim() !== "";

  return (
    <Card
      title={role === "customer" ? t("partners.newCustomer") : t("partners.newSupplier")}
      description={t("partners.newHelp")}
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          if (ready) create.mutate(form);
        }}
      >
        {create.isError && (
          <Alert tone="danger" title={t("partners.createFailed")}>{errorText(create.error)}</Alert>
        )}
        {create.isSuccess && (
          <Alert tone="info" title={t("partners.created")}>{t("partners.createdHelp")}</Alert>
        )}

        <div className="flex flex-col gap-3 sm:flex-row">
          <Input
            label={t("partners.code")}
            value={form.code}
            onChange={(event) => setForm({ ...form, code: event.target.value })}
          />
          <Input
            label={t("partners.name")}
            value={form.name}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
          />
        </div>
        <div className="flex flex-col gap-3 sm:flex-row">
          <Input
            label={t("partners.phone")}
            value={form.phone}
            onChange={(event) => setForm({ ...form, phone: event.target.value })}
            placeholder={t("partners.phoneHint")}
          />
          <Input
            label={t("partners.email")}
            type="email"
            value={form.email}
            onChange={(event) => setForm({ ...form, email: event.target.value })}
          />
        </div>

        <div className="flex justify-end">
          <Button type="submit" disabled={!ready || create.isPending}>
            {t("partners.save")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
