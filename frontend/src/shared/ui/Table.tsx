import type { ReactNode } from "react";
import { cn } from "./cn";

export interface Column<T> {
  key: string;
  header: string;
  /** Renders the cell. Returning a string is fine; formatting belongs to the caller. */
  cell: (row: T) => ReactNode;
  /** Numeric columns align to the inline end so digits line up in both directions. */
  numeric?: boolean;
}

export interface TableProps<T> {
  columns: Array<Column<T>>;
  rows: T[];
  rowKey: (row: T) => string;
  /** Rendered in place of the body when there are no rows. */
  empty?: ReactNode;
  caption?: string;
  onRowClick?: (row: T) => void;
  selectedKey?: string;
}

/**
 * A plain data table.
 *
 * Not virtualized: virtualization arrives with the first grid that has enough rows to need it,
 * and adding it now would mean choosing a windowing strategy against imagined data. The
 * component boundary is where it lands when that happens.
 */
export function Table<T>({
  columns,
  rows,
  rowKey,
  empty,
  caption,
  onRowClick,
  selectedKey,
}: TableProps<T>) {
  if (rows.length === 0 && empty) {
    return <>{empty}</>;
  }

  return (
    <div className="w-full overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        {caption ? <caption className="sr-only">{caption}</caption> : null}
        <thead>
          <tr className="border-b border-border">
            {columns.map((column) => (
              <th
                key={column.key}
                scope="col"
                className={cn(
                  "px-3 py-2 font-medium text-text-muted",
                  column.numeric ? "text-end" : "text-start",
                )}
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const key = rowKey(row);
            const selected = key === selectedKey;
            return (
              <tr
                key={key}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                // A clickable row must be reachable and activatable by keyboard, or the table
                // is mouse-only — unacceptable in a keyboard-first POS (§31).
                tabIndex={onRowClick ? 0 : undefined}
                role={onRowClick ? "button" : undefined}
                onKeyDown={
                  onRowClick
                    ? (event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          onRowClick(row);
                        }
                      }
                    : undefined
                }
                aria-current={selected || undefined}
                className={cn(
                  "border-b border-border",
                  onRowClick && "cursor-pointer hover:bg-surface-raised",
                  selected && "bg-surface-raised",
                )}
              >
                {columns.map((column) => (
                  <td
                    key={column.key}
                    className={cn(
                      "px-3 py-2 text-text",
                      column.numeric ? "text-end tabular-nums" : "text-start",
                    )}
                  >
                    {column.cell(row)}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
