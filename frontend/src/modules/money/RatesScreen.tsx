import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { Can } from "@/app/session/Can";
import {
  currencies,
  PERMISSIONS,
  rates as loadRates,
  setRate,
  type Rate,
} from "@/lib/wails";
import {
  Alert,
  Badge,
  Button,
  Card,
  EmptyState,
  Input,
  PageHeader,
  Select,
  Table,
} from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Exchange rates for one currency pair.
 *
 * # What this screen is for, and what it deliberately is not
 *
 * A shop in a country whose currency moves weekly needs three things: to record today's rate, to
 * see which rate a document written today will actually use, and to see what it replaced. This
 * shows those three and nothing else.
 *
 * It is NOT a rate feed. Nothing here reaches the network — Mizan runs with no connection at all
 * (§0), so "live rates" would mean an internet dependency the whole product is built to avoid.
 * The rate a shopkeeper types is the rate they were quoted, which in a market with a parallel
 * rate is the only number that is actually true for them.
 *
 * # Append-only, and the screen says so
 *
 * Correcting a rate records it again with the same date; the newer row wins and the mistake stays
 * visible. That is what makes "why did this invoice convert at that rate" answerable a year
 * later — so the history is the main content of this screen rather than a footnote under a single
 * current figure.
 */
export function RatesScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [from, setFrom] = useState("USD");
  const [to, setTo] = useState("SYP");

  const options = useQuery({ queryKey: ["money", "currencies"], queryFn: currencies });
  const history = useQuery({
    queryKey: ["money", "rates", from, to],
    queryFn: () => loadRates(from, to),
    enabled: from !== "" && to !== "" && from !== to,
  });

  const choices = (options.data ?? []).map((currency) => ({
    value: currency.code,
    label: `${currency.code} — ${currency.name}`,
  }));

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title={t("rates.title")} description={t("rates.help")} />

      <Card title={t("rates.pair")}>
        <div className="flex flex-wrap items-end gap-3">
          <Field label={t("rates.from")}>
            <Select
              value={from}
              onValueChange={setFrom}
              ariaLabel={t("rates.from")}
              options={choices}
            />
          </Field>
          <Field label={t("rates.to")}>
            <Select value={to} onValueChange={setTo} ariaLabel={t("rates.to")} options={choices} />
          </Field>
        </div>

        {from === to ? (
          // Not an error — just nothing to say. A currency is worth one of itself, and showing
          // an empty table under a valid selection would read as "no rates recorded".
          <p className="mt-3 text-sm text-text-muted">{t("rates.samePair")}</p>
        ) : null}
      </Card>

      <Can permission={PERMISSIONS.accountManage}>
        <NewRateForm
          from={from}
          to={to}
          disabled={from === to}
          onSaved={() => {
            void queryClient.invalidateQueries({ queryKey: ["money", "rates", from, to] });
          }}
        />
      </Can>

      {history.isError ? (
        <Alert tone="danger" title={t("rates.failed")}>{errorText(history.error)}</Alert>
      ) : null}

      {history.data ? (
        <Table<Rate>
          caption={t("rates.history")}
          rowKey={(row) => `${row.validFrom}:${row.recordedAt}`}
          rows={history.data}
          empty={<EmptyState title={t("rates.none")} />}
          columns={[
            {
              key: "validFrom",
              header: t("rates.validFrom"),
              cell: (row) => <span className="tabular-nums">{row.validFrom}</span>,
            },
            {
              key: "rate",
              header: t("rates.rate"),
              cell: (row) => (
                <span className="font-medium tabular-nums text-text">{trimRate(row.rate)}</span>
              ),
            },
            {
              key: "state",
              header: t("rates.state"),
              /*
               * Which row the engine will actually use, marked by the BACKEND rather than
               * derived here. A screen that re-implemented the resolution order would
               * eventually disagree with the till, and nobody would notice until they compared
               * a receipt against a report.
               */
              cell: (row) =>
                row.inForce ? (
                  <Badge tone="success">{t("rates.inForce")}</Badge>
                ) : (
                  <span className="text-xs text-text-muted">{t("rates.superseded")}</span>
                ),
            },
            { key: "source", header: t("rates.source"), cell: (row) => row.source },
          ]}
        />
      ) : null}
    </section>
  );
}

function NewRateForm({
  from,
  to,
  disabled,
  onSaved,
}: {
  from: string;
  to: string;
  disabled: boolean;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [rate, setRateValue] = useState("");
  const [validFrom, setValidFrom] = useState("");

  const save = useMutation({
    mutationFn: () =>
      setRate({ from, to, rate, rateTypeCode: "", validFrom, source: "" }),
    onSuccess: () => {
      setRateValue("");
      setValidFrom("");
      onSaved();
    },
  });

  return (
    <Card title={t("rates.record")} description={t("rates.recordHelp", { from, to })}>
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          save.mutate();
        }}
      >
        <Input
          label={t("rates.rateFor", { from, to })}
          value={rate}
          /*
           * `inputMode="decimal"` and a text input, never type="number".
           *
           * A number input hands back a JavaScript number, and a rate is scaled by 10⁹ — the
           * product of a rate and an amount leaves exact-integer range long before anything
           * looks wrong. The string goes to Go and is parsed there, where the arithmetic is
           * exact.
           */
          inputMode="decimal"
          onChange={(event) => setRateValue(event.target.value)}
          hint={t("rates.rateHint")}
          required
        />
        <Input
          label={t("rates.validFrom")}
          type="date"
          value={validFrom}
          onChange={(event) => setValidFrom(event.target.value)}
          hint={t("rates.validFromHint")}
        />
        <Button type="submit" disabled={disabled || rate.trim() === ""} loading={save.isPending}>
          {t("rates.save")}
        </Button>
      </form>

      {save.isError ? (
        <div className="mt-3">
          <Alert tone="danger" title={t("rates.saveFailed")}>{errorText(save.error)}</Alert>
        </div>
      ) : null}
    </Card>
  );
}

/** Drops trailing zeros so 15000.000000000 reads as 15000. */
function trimRate(rate: string): string {
  if (!rate.includes(".")) return rate;
  return rate.replace(/0+$/, "").replace(/\.$/, "");
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium text-text">{label}</span>
      {children}
    </div>
  );
}
