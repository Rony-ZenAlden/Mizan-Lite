import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { dashboard, type Tile } from "@/lib/wails";
import { StatTile } from "@/shared/ui";
import { formatMinor } from "@/modules/accounting/money";

/**
 * The figures for one domain, above the screen that domain is worked on.
 *
 * # Why this reads the dashboard rather than its own binding
 *
 * Every tile here already exists, already has a source, and is already assembled in the
 * composition root. A per-screen KPI binding would be a second implementation of revenue, and a
 * shopkeeper comparing the home screen with the sales screen would find two answers and no rule
 * for choosing which is right.
 *
 * It costs one query, shared: the query key is the same one `DashboardScreen` uses, so opening
 * the sales screen after the home screen serves from cache rather than asking again.
 *
 * # Why a failed tile is not hidden
 *
 * A tile whose module could not answer says so. Dropping it would leave a strip that looks
 * complete while missing the one figure that matters, and zero is a real answer — a shop that
 * sold nothing today has revenue of zero — so a failure rendered as zero is a lie the reader
 * cannot detect.
 */
export function KpiStrip({ source, only }: { source: string; only?: string[] }) {
  const { t } = useTranslation();

  const board = useQuery({ queryKey: ["insight", "dashboard"], queryFn: () => dashboard() });

  // Silent while loading and silent on failure. This is a HEADER on somebody else's screen: an
  // error banner here would push the actual work down the page and, worse, would be read as the
  // screen below having failed.
  if (!board.data) return null;

  const tiles = board.data.tiles.filter(
    (tile) => tile.source === source && (!only || only.includes(tile.key)),
  );
  if (tiles.length === 0) return null;

  return (
    <div
      className="grid grid-cols-2 gap-3 lg:grid-cols-4"
      aria-label={t("dashboard.title")}
      role="group"
    >
      {tiles.map((tile) => (
        <StatTile
          key={tile.key}
          label={t(`dashboard.tile.${tile.key}`)}
          value={valueOf(tile, t)}
          /*
           * The caption says WHICH RANGE, on every tile, because a periodic figure and a
           * position look identical otherwise — and "stock value 40,000" read as this month's
           * purchases is the mistake this line exists to prevent.
           */
          caption={t(tile.periodic ? "dashboard.periodic" : "dashboard.asAt")}
          tone={toneOf(tile)}
        />
      ))}
    </div>
  );
}

function valueOf(tile: Tile, t: (key: string) => string): string {
  if (tile.failed) return t("dashboard.tileFailed");
  return tile.kind === "money" ? formatMinor(tile.amountMinor) : String(tile.count);
}

/**
 * Colour carries meaning here, and only where it has one.
 *
 * A count of empty shelves is bad news at one and good news at zero, so it is the one tile whose
 * tone depends on its value. Revenue is not "good" for being large — a shop reading a red figure
 * as an error would go looking for a fault that is not there.
 */
function toneOf(tile: Tile): "neutral" | "negative" | "muted" {
  if (tile.failed) return "muted";
  if (tile.key === "inventory.out_of_stock" && tile.count > 0) return "negative";
  return "neutral";
}
