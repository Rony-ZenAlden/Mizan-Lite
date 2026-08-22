import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { Can } from "@/app/session/Can";
import {
  PERMISSIONS,
  createUser,
  roles as loadRoles,
  setUserActive,
  users as loadUsers,
  type NewUser,
  type User,
} from "@/lib/wails";
import { Alert, Button, Dialog, EmptyState, Input, PageHeader, Select, Table } from "@/shared/ui";
import { useErrorText } from "./useAdminError";

const USERS_KEY = ["identity", "users"] as const;

/**
 * The people who can sign in.
 *
 * Deactivate is offered even where the backend will refuse it — the last active user, yourself,
 * the last administrator. A control that explains why it refused teaches the rule; a missing
 * one teaches nothing, and the user is left guessing why the person is still there.
 */
export function UsersScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);

  const users = useQuery({ queryKey: USERS_KEY, queryFn: loadUsers });

  const toggle = useMutation({
    mutationFn: ({ id, active }: { id: string; active: boolean }) => setUserActive(id, active),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: USERS_KEY }),
  });

  if (users.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (users.isError) {
    return <Alert tone="danger" title={t("admin.users.failed")}>{errorText(users.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-4">
      <header className="flex items-center justify-between gap-4">
        <PageHeader title={t("admin.users.title")} />
        <Can permission={PERMISSIONS.userManage}>
          <Button onClick={() => setCreating(true)}>{t("admin.users.add")}</Button>
        </Can>
      </header>

      {toggle.isError ? (
        <Alert tone="danger" title={t("admin.users.changeFailed")}>{errorText(toggle.error)}</Alert>
      ) : null}

      <Table<User>
        caption={t("admin.users.title")}
        rowKey={(user) => user.id}
        rows={users.data}
        empty={<EmptyState title={t("admin.users.none")} />}
        columns={[
          { key: "username", header: t("admin.users.username"), cell: (user) => user.username },
          { key: "name", header: t("admin.users.name"), cell: (user) => user.displayName },
          {
            key: "status",
            header: t("admin.users.status"),
            cell: (user) => (user.isActive ? t("admin.users.active") : t("admin.users.inactive")),
          },
          {
            key: "actions",
            header: t("admin.users.actions"),
            cell: (user) => (
              <Can permission={PERMISSIONS.userManage}>
                <Button
                  variant="ghost"
                  size="sm"
                  loading={toggle.isPending && toggle.variables?.id === user.id}
                  onClick={() => toggle.mutate({ id: user.id, active: !user.isActive })}
                >
                  {user.isActive ? t("admin.users.deactivate") : t("admin.users.activate")}
                </Button>
              </Can>
            ),
          },
        ]}
      />

      {creating ? <CreateUserDialog onClose={() => setCreating(false)} /> : null}
    </section>
  );
}

/**
 * The create-user form.
 *
 * The password is set by an administrator, so the backend forces must-change (1.11 D1) and the
 * form says so — otherwise the person is surprised at their first sign-in by a demand nobody
 * mentioned.
 */
function CreateUserDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();
  const roles = useQuery({ queryKey: ["identity", "roles"], queryFn: loadRoles });

  const [form, setForm] = useState<NewUser>({
    username: "",
    displayName: "",
    email: "",
    password: "",
    roleCode: "",
  });

  const create = useMutation({
    mutationFn: () => createUser(form),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: USERS_KEY });
      onClose();
    },
  });

  const ready = form.username.trim() !== "" && form.password !== "";

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={t("admin.users.add")}
      description={t("admin.users.add.help")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("action.cancel")}
          </Button>
          <Button onClick={() => create.mutate()} disabled={!ready} loading={create.isPending}>
            {t("admin.users.create")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {create.isError ? (
          <Alert tone="danger" title={t("admin.users.createFailed")}>{errorText(create.error)}</Alert>
        ) : null}
        <Input
          label={t("admin.users.username")}
          value={form.username}
          onChange={(event) => setForm({ ...form, username: event.target.value })}
          autoComplete="off"
          required
        />
        <Input
          label={t("admin.users.name")}
          value={form.displayName}
          onChange={(event) => setForm({ ...form, displayName: event.target.value })}
        />
        <Input
          label={t("admin.users.email")}
          type="email"
          value={form.email}
          onChange={(event) => setForm({ ...form, email: event.target.value })}
        />
        <Input
          label={t("admin.users.password")}
          type="password"
          value={form.password}
          onChange={(event) => setForm({ ...form, password: event.target.value })}
          autoComplete="new-password"
          hint={t("admin.users.password.hint")}
          required
        />
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium text-text">{t("admin.users.role")}</span>
          <Select
            value={form.roleCode}
            onValueChange={(roleCode) => setForm({ ...form, roleCode })}
            ariaLabel={t("admin.users.role")}
            options={(roles.data ?? []).map((role) => ({ value: role.code, label: role.name }))}
          />
          <span className="text-xs text-text-muted">{t("admin.users.role.hint")}</span>
        </div>
      </div>
    </Dialog>
  );
}
