import { useEffect, useState } from "react";
import { Plus, Pencil, Trash2, Cctv, Layers } from "lucide-react";
import { SettingsRow } from "./ui/Layout";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import {
  useCameraGroups,
  useCreateGroup,
  useUpdateGroup,
  useDeleteGroup,
  type CameraGroup,
} from "../hooks/useGroups";
import { useCameras } from "../hooks/useCameras";

export function CameraGroupsManager() {
  const { data: groups, isLoading } = useCameraGroups();
  const { data: cameras } = useCameras();
  const del = useDeleteGroup();
  const [editing, setEditing] = useState<CameraGroup | "new" | null>(null);
  const [confirmDel, setConfirmDel] = useState<CameraGroup | null>(null);

  const camList = cameras ?? [];
  const nameOf = (id: string) => camList.find((c) => c.id === id)?.name;

  const flush = "[&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0";

  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-fg">Camera groups</h3>
          <p className="mt-1 text-xs leading-relaxed text-fg-faint">
            Named sets of cameras for the mobile dashboard&apos;s group selector. Everyone always has a built-in “All
            Cameras” group.
          </p>
        </div>
        {editing === null && (
          <button className="btn-outline shrink-0" onClick={() => setEditing("new")}>
            <Plus className="h-4 w-4" />
            New group
          </button>
        )}
      </div>

      {editing !== null ? (
        <GroupEditor
          group={editing === "new" ? null : editing}
          cameras={camList}
          onDone={() => setEditing(null)}
        />
      ) : (
        <div className="overflow-x-auto">
          <table className={`tbl table-fixed ${flush}`}>
            <thead>
              <tr>
                <th className="w-[38%]">Name</th>
                <th>Cameras</th>
                <th className="w-[84px]" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {/* Built-in group (read-only) */}
              <tr>
                <td>
                  <div className="flex min-w-0 items-center gap-2.5">
                    <Layers className="h-4 w-4 shrink-0 text-brand" />
                    <span className="truncate text-fg">All Cameras</span>
                  </div>
                </td>
                <td>
                  <span className="block truncate text-fg-faint">{camList.length} cameras · built-in</span>
                </td>
                <td />
              </tr>
              {(groups ?? []).map((g) => {
                const names = g.cameraIds.map(nameOf).filter(Boolean) as string[];
                return (
                  <tr key={g.id} className="hover:bg-white/[0.02]">
                    <td>
                      <div className="flex min-w-0 items-center gap-2.5">
                        <Layers className="h-4 w-4 shrink-0 text-fg-muted" />
                        <span className="truncate text-fg">{g.name}</span>
                      </div>
                    </td>
                    <td>
                      <span
                        className={`block truncate ${g.cameraIds.length === 0 ? "text-fg-faint" : ""}`}
                        title={names.join(", ")}
                      >
                        {g.cameraIds.length === 0 ? "No cameras" : names.join(", ")}
                      </span>
                    </td>
                    <td>
                      <div className="flex justify-end gap-1">
                        <button className="btn-icon" onClick={() => setEditing(g)} aria-label="Edit" title="Edit">
                          <Pencil className="h-4 w-4" />
                        </button>
                        <button
                          className="btn-icon hover:text-danger"
                          onClick={() => setConfirmDel(g)}
                          aria-label="Delete"
                          title="Delete"
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!isLoading && (groups ?? []).length === 0 && (
            <p className="flex items-center gap-3 border-b border-line py-4 text-sm text-fg-faint">
              <Layers className="h-4 w-4 shrink-0" />
              No custom groups yet.
            </p>
          )}
        </div>
      )}

      <ConfirmDialog
        open={!!confirmDel}
        danger
        busy={del.isPending}
        title="Delete group?"
        message={`Delete the “${confirmDel?.name}” group? The cameras themselves are not affected.`}
        confirmLabel="Delete"
        onConfirm={async () => {
          if (confirmDel) await del.mutateAsync(confirmDel.id);
          setConfirmDel(null);
        }}
        onCancel={() => setConfirmDel(null)}
      />
    </>
  );
}

function GroupEditor({
  group,
  cameras,
  onDone,
}: {
  group: CameraGroup | null;
  cameras: { id: string; name: string; enabled: boolean }[];
  onDone: () => void;
}) {
  const create = useCreateGroup();
  const update = useUpdateGroup();
  const [name, setName] = useState(group?.name ?? "");
  const [sel, setSel] = useState<string[]>(group?.cameraIds ?? []);
  useEffect(() => {
    setName(group?.name ?? "");
    setSel(group?.cameraIds ?? []);
  }, [group]);

  const toggle = (id: string) => setSel((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]));
  const busy = create.isPending || update.isPending;

  async function save() {
    if (!name.trim()) return;
    if (group) await update.mutateAsync({ id: group.id, name: name.trim(), cameraIds: sel });
    else await create.mutateAsync({ name: name.trim(), cameraIds: sel });
    onDone();
  }

  return (
    <div className="panel">
      <h4 className="text-sm font-semibold text-fg">{group ? `Edit “${group.name}”` : "New group"}</h4>
      <div className="mt-2 divide-y divide-line">
        <SettingsRow label="Group name">
          <input
            className="input max-w-sm"
            value={name}
            placeholder="e.g. Outdoors"
            onChange={(e) => setName(e.target.value)}
            aria-label="Group name"
          />
        </SettingsRow>

        <SettingsRow label="Cameras" hint={`${sel.length} of ${cameras.length} selected`} align="start">
          <div className="grid max-w-2xl grid-cols-1 gap-x-6 sm:grid-cols-2">
            {cameras.map((c) => {
              const on = sel.includes(c.id);
              return (
                <label
                  key={c.id}
                  className="flex h-9 cursor-pointer items-center gap-2.5 rounded-md px-1 text-sm hover:bg-white/[0.03]"
                >
                  <input type="checkbox" checked={on} onChange={() => toggle(c.id)} />
                  <Cctv className="h-4 w-4 shrink-0 text-fg-faint" />
                  <span className={`truncate ${on ? "text-fg" : "text-fg-muted"}`}>{c.name}</span>
                </label>
              );
            })}
          </div>
        </SettingsRow>
      </div>

      <div className="mt-2 flex justify-end gap-2 border-t border-line pt-4">
        <button className="btn-ghost" onClick={onDone} disabled={busy}>
          Cancel
        </button>
        <button className="btn-primary" onClick={save} disabled={busy || !name.trim()}>
          {busy ? "Saving…" : group ? "Save group" : "Create group"}
        </button>
      </div>
    </div>
  );
}
