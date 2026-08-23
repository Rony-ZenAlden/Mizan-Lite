import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { dashboard, type Tile } from "@/lib/wails";
import { Alert, Badge, EmptyState, PageHeader } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

/**
 * The home screen: several modules' answers, each labelled with who answered.
 *
 * # Why every tile says its source and its range
 *
 * This is where somebody first notices a number is wrong, and "revenue 4,500" with no indication
 * of what it counts sends them looking through five screens.
 *
 * The range matters as much: sales is a figure OVER a period, stock value is a position AS AT
 * now. Rendering them side by side without saying which is which is how "stock value 40,000" gets
 * read as this month's purchases.
 */
export function DashboardScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  // An empty range asks the backend for month-to-date. Computing it here would use the browser's
  // timezone, which can be a day out from the one the books are kept in.
  const board = useQuery({ queryKey: ["insight", "dashboard"], queryFn: () => dashboard() });

  if (board.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (board.isError) {
    return (
      <Alert tone="danger" title={t("dashboard.failed")}>{errorText(board.error)}</Alert>
    );
  }

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title={t("dashboard.title")}
        description={t("dashboard.range", { from: board.data.from, to: board.data.to })}
      />

      {board.data.tiles.length === 0 ? (
        <EmptyState title={t("dashboard.none")} />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {board.data.tiles.map((tile) => (
            <TileCard key={tile.key} tile={tile} />
          ))}
        </div>
      )}
    </section>
  );
}

function TileCard({ tile }: { tile: Tile }) {
  const { t } = useTranslation();

  return (
    <article className="flex flex-col gap-2 card p-4">
      <div className="flex items-start justify-between gap-2">
        <h3 className="text-sm font-medium text-text">{t(`dashboard.tile.${tile.key}`)}</h3>
        {/* The range, on every tile. A periodic figure and a position look identical otherwise. */}
        <Badge tone={tile.periodic ? "neutral" : "info"}>
          {t(tile.periodic ? "dashboard.periodic" : "dashboard.asAt")}
        </Badge>
      </div>

      {tile.failed ? (
        /*
         * A tile that could not answer says so rather than showing zero.
         *
         * Zero is a real figure — a shop that sold nothing today has revenue of zero — and
         * rendering a failure as one would be a lie the reader has no way to detect.
         */
        <p className="text-sm text-danger">{t("dashboard.tileFailed")}</p>
      ) : (
        <p className="text-2xl font-semibold tabular-nums text-text">
          {tile.kind === "money" ? formatMinor(tile.amountMinor) : tile.count}
        </p>
      )}

      <p className="text-xs text-text-muted">{t(`dashboard.source.${tile.source}`)}</p>
    </article>
  );
}
