import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product, StockLevel, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { formErrors, typedQuantity } from "./forms";

/**
 * Opens whole packages into their loose content (L2 §5.7). What is on hand afterwards is shown from Go's answer — the
 * form never multiplies packages by content itself.
 */
export function OpenPackageDialog({
  pkg,
  name,
  pkgUnit,
  contentName,
  contentUnit,
  onDone,
  onClose,
}: {
  pkg: Product;
  name: string;
  pkgUnit: Unit;
  contentName: string;
  contentUnit: Unit;
  onDone: (levels: StockLevel[]) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [packages, setPackages] = useState("1");
  const [opened, setOpened] = useState<StockLevel[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const code = typedQuantity(packages, 0);
  const errors = formErrors(error, errorText);
  const amount = (quantity: string, unit: Unit) => `${formatDecimal(quantity, locale)} ${tDynamic(`uom.${unit.code}`)}`;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const levels = await client.stock.openPackage({ packageProductId: pkg.id, packages });
      setOpened(levels);
      onDone(levels);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("stock.open_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <p className="text-sm">
          {t("stock.open_into", {
            quantity: formatDecimal(pkg.packageContentQuantity, locale),
            unit: tDynamic(`uom.${contentUnit.code}`),
            content: contentName,
          })}
        </p>
        <TextField
          label={t("stock.packages")}
          value={packages}
          onChange={(e) => setPackages(e.target.value)}
          error={(code ? tDynamic(code, { decimals: "0" }) : undefined) ?? errors.field("packages")}
          inputMode="numeric"
          dir="ltr"
          required
        />
        {opened && opened.length === 2 ? (
          <p role="status" className="text-sm text-success">
            {t("stock.opened", { package: amount(opened[0]!.onHand, pkgUnit), content: amount(opened[1]!.onHand, contentUnit) })}
          </p>
        ) : null}
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t(opened ? "action.close" : "action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || packages === "" || Boolean(code)}>
            {t("stock.open_package")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
