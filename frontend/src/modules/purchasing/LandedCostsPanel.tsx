import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { applyLandedCost, landedCosts, type LandedCost } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

/**
 * Freight, customs and clearing added to what a delivery cost.
 *
 * # Recording a charge and APPLYING it are two decisions
 *
 * Recording that freight was charged is bookkeeping. Deciding it belongs in the cost of these
 * particular goods — and therefore in the price every future sale is measured against — is a
 * judgement somebody makes. 6.6 made them two service calls for that reason.
 *
 * The screen keeps them apart: a charge is listed as pending until somebody applies it, and
 * applying is a button rather than a consequence of saving.
 */
export function LandedCostsPanel({ receiptId }: { receiptId: string }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const charges = useQuery({
    queryKey: ["purchasing", "landed-costs", receiptId],
    queryFn: () => landedCosts(receiptId),
    enabled: receiptId !== "",
  });

  const apply = useMutation({
    mutationFn: applyLandedCost,
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["purchasing", "landed-costs", receiptId] }),
  });

  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-sm font-medium text-text">{t("landedCosts.title")}</h3>

      {charges.isError && (
        <Alert tone="danger" title={t("landedCosts.failed")}>{errorText(charges.error)}</Alert>
      )}
      {apply.isError && (
        <Alert tone="danger" title={t("landedCosts.applyFailed")}>{errorText(apply.error)}</Alert>
      )}

      <Table<LandedCost>
        caption={t("landedCosts.title")}
        rowKey={(row) => row.id}
        rows={charges.data ?? []}
        empty={<EmptyState title={t("landedCosts.none")} />}
        columns={[
          {
            key: "type",
            header: t("landedCosts.charge"),
            cell: (row) => row.description || row.chargeType,
          },
          {
            key: "basis",
            /*
             * The BASIS decides where the money lands — by value, by quantity, by weight — so it
             * is shown rather than hidden behind the total. Two charges of the same amount on the
             * same delivery can cost two products very differently.
             */
            header: t("landedCosts.basis"),
            cell: (row) => (
              <Badge tone="neutral">{t(`landedCosts.basis.${row.basis}`)}</Badge>
            ),
          },
          {
            key: "amount",
            header: t("landedCosts.amount"),
            cell: (row) => <span className="tabular-nums">{formatMinor(row.amountMinor)}</span>,
          },
          {
            key: "status",
            header: "",
            cell: (row) =>
              row.status === "applied" ? (
                <Badge tone="success">{t("landedCosts.applied")}</Badge>
              ) : (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => apply.mutate(row.id)}
                  disabled={apply.isPending}
                >
                  {t("landedCosts.apply")}
                </Button>
              ),
          },
        ]}
      />
    </section>
  );
}
