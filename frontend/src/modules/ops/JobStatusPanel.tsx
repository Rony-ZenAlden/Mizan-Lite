import { useEffect, useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { isBindingError, jobs as loadJobs, runs as loadRuns, type JobState, type RunRecord } from "@/lib/wails";
import { Alert, EmptyState, Skeleton, Table, type Column } from "@/shared/ui";

/**
 * The background-job diagnostics panel (Step 0.11 D7), closing Phase 0 DoD item 5.
 *
 * §24.2 is why this exists in Phase 0 rather than Phase 9: "invisible background failures are
 * how customers lose backups without knowing." The query API was built and tested in 0.7; this
 * is the first thing to read it.
 *
 * It is also the first screen to exercise all four designed states for real — loading, error,
 * empty, and populated.
 */
export function JobStatusPanel() {
  const { t, locale } = useTranslation();
  const [state, setState] = useState<{ jobs: JobState[]; error: string | null } | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  useEffect(() => {
    loadJobs()
      .then((list) => setState({ jobs: list, error: null }))
      .catch((error: unknown) =>
        setState({
          jobs: [],
          error: isBindingError(error) ? t(error.messageKey, error.params) : t("app.unknown_error"),
        }),
      );
  }, [t]);

  if (!state) {
    return (
      <div className="flex flex-col gap-2" aria-busy="true">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }

  if (state.error) {
    return <Alert tone="danger" title={t("ops.jobs.error")}>{state.error}</Alert>;
  }

  if (state.jobs.length === 0) {
    return <EmptyState title={t("ops.jobs.empty")} description={t("ops.jobs.emptyHint")} />;
  }

  const columns: Array<Column<JobState>> = [
    { key: "key", header: t("ops.jobs.name"), cell: (job) => <span className="font-mono">{job.key}</span> },
    { key: "schedule", header: t("ops.jobs.schedule"), cell: (job) => job.schedule },
    {
      key: "status",
      header: t("ops.jobs.status"),
      cell: (job) => <StatusBadge status={job.lastStatus} label={t(statusKey(job.lastStatus))} />,
    },
    { key: "last", header: t("ops.jobs.lastRun"), cell: (job) => formatTime(job.lastRunAt, locale) },
    { key: "next", header: t("ops.jobs.nextRun"), cell: (job) => formatTime(job.nextRunAt, locale) },
  ];

  return (
    <div className="flex flex-col gap-6">
      <Table
        caption={t("ops.jobs.caption")}
        columns={columns}
        rows={state.jobs}
        rowKey={(job) => job.key}
        selectedKey={selected ?? undefined}
        onRowClick={(job) => setSelected(job.key === selected ? null : job.key)}
      />
      {selected ? <RecentRuns jobKey={selected} /> : null}
    </div>
  );
}

function RecentRuns({ jobKey }: { jobKey: string }) {
  const { t, locale } = useTranslation();
  const [records, setRecords] = useState<RunRecord[] | null>(null);

  useEffect(() => {
    setRecords(null);
    loadRuns(jobKey, 10)
      .then(setRecords)
      .catch(() => setRecords([]));
  }, [jobKey]);

  if (!records) return <Skeleton className="h-24 w-full" />;
  if (records.length === 0) {
    return <EmptyState title={t("ops.runs.empty")} description={t("ops.runs.emptyHint")} />;
  }

  const columns: Array<Column<RunRecord>> = [
    { key: "started", header: t("ops.runs.started"), cell: (run) => formatTime(run.startedAt, locale) },
    {
      key: "status",
      header: t("ops.jobs.status"),
      cell: (run) => <StatusBadge status={run.status} label={t(statusKey(run.status))} />,
    },
    { key: "attempt", header: t("ops.runs.attempt"), cell: (run) => run.attempt, numeric: true },
    { key: "trigger", header: t("ops.runs.trigger"), cell: (run) => run.triggeredBy },
  ];

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-sm font-medium text-text">{t("ops.runs.title", { job: jobKey })}</h3>
      <Table columns={columns} rows={records} rowKey={(run) => run.runId} />
    </section>
  );
}

const STATUS_TONE: Record<string, string> = {
  succeeded: "bg-success-subtle text-success",
  running: "bg-info-subtle text-info",
  failed: "bg-danger-subtle text-danger",
  timeout: "bg-warning-subtle text-warning",
  cancelled: "bg-surface-sunken text-text-muted",
};

function StatusBadge({ status, label }: { status: string; label: string }) {
  if (!status) return <span className="text-text-muted">—</span>;
  return (
    <span
      className={`inline-block rounded-sm px-2 py-0.5 text-xs ${
        STATUS_TONE[status] ?? "bg-surface-sunken text-text-muted"
      }`}
    >
      {label}
    </span>
  );
}

/** Maps a status to its catalog key, falling back to the raw value for an unknown one. */
function statusKey(status: string): string {
  return status ? `ops.status.${status}` : "ops.status.never";
}

/**
 * Formats a CHAR(24) UTC timestamp for display.
 *
 * The backend never formats a date, because a formatted date is prose (§22.2) — it sends the
 * portable instant and the frontend renders it with Intl in the active locale.
 */
function formatTime(value: string, locale: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "medium" }).format(date);
}
