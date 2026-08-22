import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import {
  addExpenseLine,
  draftExpense,
  expenseCategories,
  recordExpense,
} from "@/lib/wails";
import { Alert, Button, Card, Input, Select } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Recording an expense (Step 10.14).
 *
 * # Three calls behind one button
 *
 * The service models an expense the way the books need it: a draft, then lines, then a posting
 * that is atomic and numbered. `draftExpense`, `addExpenseLine` and `recordExpense` have existed
 * since Phase 7 and the screen was read-only, so an expense could be listed and never entered.
 *
 * A person paying rent does not have a draft. They have an amount, a payee, and whether it is
 * paid — so the form asks that, and does the three calls in order.
 *
 * # Why the CATEGORY is the only structural question
 *
 * It decides which account the money lands in, which is the whole of 7.1's design: the module
 * contains no accounting logic, and the category is how a person expresses the accounting without
 * choosing an account. Asking it is not bureaucracy; it is the one thing only they know.
 */
export function NewExpenseForm({ onRecorded }: { onRecorded?: () => void }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [payee, setPayee] = useState("");
  const [categoryId, setCategoryId] = useState("");
  const [amount, setAmount] = useState("");
  const [paidNow, setPaidNow] = useState("immediate");
  const [reference, setReference] = useState("");

  const categories = useQuery({
    queryKey: ["expenses", "categories"],
    queryFn: expenseCategories,
  });

  const record = useMutation({
    mutationFn: async () => {
      const today = new Date().toISOString().slice(0, 10);
      const draft = await draftExpense({
        partnerId: "",
        payeeName: payee.trim(),
        expenseDate: today,
        reference: reference.trim(),
        description: "",
        settlement: paidNow,
        // Cash when paid on the spot; the posting rule turns the METHOD into an account, so this
        // screen still names none (§20.3).
        paidMethod: paidNow === "immediate" ? "cash" : "",
        dueDate: paidNow === "immediate" ? "" : today,
        currency: "",
      });
      await addExpenseLine({
        expenseId: draft.id,
        categoryId,
        description: reference.trim(),
        netMinor: toMinor(amount),
      });
      // RECORDED, not left as a draft. A draft expense is invisible in every report and in the
      // partner's balance — a screen that saved one would look like it worked and change nothing.
      return recordExpense(draft.id);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["expenses"] });
      setPayee("");
      setAmount("");
      setReference("");
      onRecorded?.();
    },
  });

  const ready =
    payee.trim() !== "" && categoryId !== "" && amount.trim() !== "" && !Number.isNaN(Number(amount));

  return (
    <Card title={t("expenses.new")} description={t("expenses.newHelp")}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          if (ready) record.mutate();
        }}
      >
        {record.isError && (
          <Alert tone="danger" title={t("expenses.createFailed")}>{errorText(record.error)}</Alert>
        )}
        {record.isSuccess && (
          <Alert tone="info" title={t("expenses.recorded")}>{t("expenses.recordedHelp")}</Alert>
        )}

        <div className="flex flex-col gap-3 sm:flex-row">
          <Input
            label={t("expenses.payee")}
            value={payee}
            onChange={(event) => setPayee(event.target.value)}
            placeholder={t("expenses.payeeHint")}
          />
          <Select
            label={t("expenses.category")}
            value={categoryId}
            onValueChange={setCategoryId}
            options={[
              { value: "", label: t("expenses.chooseCategory") },
              ...(categories.data ?? []).map((c) => ({ value: c.id, label: c.name })),
            ]}
          />
        </div>

        <div className="flex flex-col gap-3 sm:flex-row">
          <Input
            label={t("expenses.amount")}
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
          />
          <Select
            label={t("expenses.settlement")}
            value={paidNow}
            onValueChange={setPaidNow}
            options={[
              { value: "immediate", label: t("expenses.paidNow") },
              { value: "on_account", label: t("expenses.onAccount") },
            ]}
          />
        </div>

        <Input
          label={t("expenses.reference")}
          value={reference}
          onChange={(event) => setReference(event.target.value)}
          placeholder={t("expenses.referenceHint")}
        />

        <div className="flex justify-end">
          <Button type="submit" disabled={!ready || record.isPending}>
            {t("expenses.save")}
          </Button>
        </div>
      </form>
    </Card>
  );
}

/**
 * Whole units to minor units, without float arithmetic.
 *
 * `Number(x) * 100` is exactly the mistake §E exists to prevent: 19.99 does not survive it. The
 * conversion is textual, so 19.99 becomes "1999" and 19.9 becomes "1990".
 */
function toMinor(value: string): string {
  const [whole = "0", fraction = ""] = value.trim().split(".");
  const scaled = `${whole}${(fraction + "00").slice(0, 2)}`.replace(/^0+(?=\d)/, "");
  return scaled === "" ? "0" : scaled;
}
