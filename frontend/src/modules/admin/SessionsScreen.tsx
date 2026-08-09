import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { Can } from "@/app/session/Can";
import { PERMISSIONS, adminSessions, revokeSession, type AdminSession } from "@/lib/wails";
import { Alert, Button, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "./useAdminError";

const SESSIONS_KEY = ["identity", "sessions"] as const;

/**
 * Who is signed in, and the ability to end it.
 *
 * The caller's OWN session is marked rather than hidden. "Sign me out everywhere" is a
 * legitimate thing to want, and ending it is permitted — but it takes effect on the very next
 * call, so it must be a decision rather than a surprise.
 */
export function SessionsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const sessions = useQuery({ queryKey: SESSIONS_KEY, queryFn: adminSessions });
  const revoke = useMutation({
    // Wrapped rather than passed by reference: TanStack Query calls mutationFn with a SECOND
    // argument (its own context), and handing it a binding wrapper directly would forward that
    // object across the IPC boundary the moment any wrapper started spreading its arguments.
    // Naming the one parameter keeps what crosses to Go exactly what we meant to send.
    mutationFn: (sessionId: string) => revokeSession(sessionId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: SESSIONS_KEY }),
  });

  if (sessions.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (sessions.isError) {
    return (
      <Alert tone="danger" title={t("admin.sessions.failed")}>{errorText(sessions.error)}</Alert>
    );
  }

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-base font-medium text-text">{t("admin.sessions.title")}</h2>
      {revoke.isError ? (
        <Alert tone="danger" title={t("admin.sessions.revokeFailed")}>{errorText(revoke.error)}</Alert>
      ) : null}

      <Table<AdminSession>
        caption={t("admin.sessions.title")}
        rowKey={(session) => session.id}
        rows={sessions.data}
        empty={<EmptyState title={t("admin.sessions.none")} />}
        columns={[
          {
            key: "who",
            header: t("admin.sessions.who"),
            cell: (session) =>
              session.current
                ? t("admin.sessions.youLabel", { name: session.displayName || session.username })
                : session.displayName || session.username,
          },
          {
            key: "device",
            header: t("admin.sessions.device"),
            cell: (session) => session.deviceInfo || "—",
          },
          { key: "seen", header: t("admin.sessions.lastSeen"), cell: (session) => session.lastSeen },
          {
            key: "actions",
            header: t("admin.sessions.actions"),
            cell: (session) => (
              <Can permission={PERMISSIONS.sessionRevoke}>
                <Button
                  variant="ghost"
                  size="sm"
                  loading={revoke.isPending && revoke.variables === session.id}
                  onClick={() => revoke.mutate(session.id)}
                >
                  {session.current ? t("admin.sessions.endMine") : t("admin.sessions.end")}
                </Button>
              </Can>
            ),
          },
        ]}
      />
    </section>
  );
}
