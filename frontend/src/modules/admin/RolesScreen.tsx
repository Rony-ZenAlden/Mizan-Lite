import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { useCan } from "@/app/session/session";
import {
  PERMISSIONS,
  grantToRole,
  permissionCatalogue,
  revokeFromRole,
  roleGrants,
  roles as loadRoles,
  type Role,
} from "@/lib/wails";
import { Alert, Checkbox, EmptyState, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "./useAdminError";

/**
 * Roles and what they may do.
 *
 * The permission list comes from the BACKEND catalogue (1.11 D5), which the startup sync wrote
 * from what every module declares. A list typed into the frontend would drift the moment a
 * module added a permission, and the symptom — "the checkbox is missing" — would appear
 * nowhere near the cause.
 */
export function RolesScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const [selected, setSelected] = useState<Role | null>(null);

  const roles = useQuery({ queryKey: ["identity", "roles"], queryFn: loadRoles });

  if (roles.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (roles.isError) {
    return <Alert tone="danger" title={t("admin.roles.failed")}>{errorText(roles.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-4">
      <PageHeader title={t("admin.roles.title")} />

      <div className="grid gap-4 lg:grid-cols-2">
        <Table<Role>
          caption={t("admin.roles.title")}
          rowKey={(role) => role.id}
          rows={roles.data}
          selectedKey={selected?.id}
          onRowClick={setSelected}
          empty={<EmptyState title={t("admin.roles.none")} />}
          columns={[
            { key: "name", header: t("admin.roles.name"), cell: (role) => role.name },
            { key: "code", header: t("admin.roles.code"), cell: (role) => role.code },
            {
              key: "kind",
              header: t("admin.roles.kind"),
              cell: (role) =>
                // Seeded roles are freely EDITABLE but never deletable: removing one would
                // orphan everyone assigned to it (1.4).
                role.isSystem ? t("admin.roles.system") : t("admin.roles.custom"),
            },
          ]}
        />

        {selected ? (
          <GrantEditor role={selected} />
        ) : (
          <EmptyState title={t("admin.roles.pick")} description={t("admin.roles.pick.help")} />
        )}
      </div>
    </section>
  );
}

/** The checkbox grid for one role's grants. */
function GrantEditor({ role }: { role: Role }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();
  const mayEdit = useCan(PERMISSIONS.roleManage);

  const grantsKey = ["identity", "roleGrants", role.id];
  const catalogue = useQuery({
    queryKey: ["identity", "permissions"],
    queryFn: permissionCatalogue,
  });
  const grants = useQuery({ queryKey: grantsKey, queryFn: () => roleGrants(role.id) });

  const toggle = useMutation({
    mutationFn: ({ code, granted }: { code: string; granted: boolean }) =>
      granted ? grantToRole(role.id, code) : revokeFromRole(role.id, code),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: grantsKey }),
  });

  if (catalogue.isPending || grants.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (catalogue.isError || grants.isError) {
    return (
      <Alert tone="danger" title={t("admin.roles.grantsFailed")}>
        {errorText(catalogue.error ?? grants.error)}
      </Alert>
    );
  }

  const held = new Set(grants.data);
  // A wildcard grant satisfies everything beneath it (1.4), so every box reads as ticked — and
  // it is shown as such rather than as an empty grid, which would suggest the administrator
  // role can do nothing.
  const wildcard = held.has("*");

  const byModule = new Map<string, typeof catalogue.data>();
  for (const entry of catalogue.data) {
    const list = byModule.get(entry.module) ?? [];
    list.push(entry);
    byModule.set(entry.module, list);
  }

  return (
    <div className="flex flex-col gap-3 rounded border border-border p-4">
      <h3 className="text-sm font-medium text-text">
        {t("admin.roles.grantsFor", { role: role.name })}
      </h3>
      {toggle.isError ? (
        <Alert tone="danger" title={t("admin.roles.grantFailed")}>{errorText(toggle.error)}</Alert>
      ) : null}
      {wildcard ? (
        <Alert tone="info" title={t("admin.roles.wildcard")}>{t("admin.roles.wildcard.help")}</Alert>
      ) : null}

      {[...byModule.entries()].map(([module, entries]) => (
        <fieldset key={module} className="flex flex-col gap-2">
          <legend className="text-xs uppercase tracking-wide text-text-muted">{module}</legend>
          {entries.map((entry) => (
            <Checkbox
              key={entry.code}
              checked={wildcard || held.has(entry.code)}
              disabled={!mayEdit || wildcard || entry.obsolete || toggle.isPending}
              onCheckedChange={(granted) => toggle.mutate({ code: entry.code, granted })}
              label={
                entry.obsolete
                  ? t("admin.roles.obsolete", { code: entry.code })
                  : t(`permissions.${entry.code}`)
              }
            />
          ))}
        </fieldset>
      ))}
    </div>
  );
}
