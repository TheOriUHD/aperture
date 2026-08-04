import { useEffect, useState } from "react";
import { Plus, Pencil, Trash2, Check, Cctv, Layers } from "lucide-react";
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
  const { data: groups } = useCameraGroups();
  const { data: cameras } = useCameras();
  const del = useDeleteGroup();
  const [editing, setEditing] = useState<CameraGroup | "new" | null>(null);
  const [confirmDel, setConfirmDel] = useState<CameraGroup | null>(null);

  const camList = cameras ?? [];
  const nameOf = (id: string) => camList.find((c) => c.id === id)?.name;

  return (
    <div className="panel">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium text-fg">Camera groups</h2>
        {editing === null && (
          <button className="btn-subtle h-8" onClick={() => setEditing("new")}>
            <Plus className="h-4 w-4" />
            New group
          </button>
        )}
      </div>

      <p className="mb-3 text-xs text-fg-faint">
        Named sets of cameras for the mobile dashboard&apos;s group selector. Everyone always has a built-in “All
        Cameras” group.
      </p>

      {editing !== null ? (
        <GroupEditor
          group={editing === "new" ? null : editing}
          cameras={camList}
          onDone={() => setEditing(null)}
        />
      ) : (
        <div className="space-y-2">
          {/* Built-in group (read-only) */}
          <div className="flex items-center gap-3 rounded-lg border border-line bg-ink-850 px-3 py-2.5">
            <Layers className="h-4 w-4 text-brand" />
            <span className="text-sm text-fg">All Cameras</span>
            <span className="ml-auto text-xs text-fg-faint">{camList.length} cameras · built-in</span>
          </div>
          {(groups ?? []).map((g) => (
            <div key={g.id} className="flex items-center gap-3 rounded-lg border border-line bg-ink-850 px-3 py-2.5">
              <Layers className="h-4 w-4 text-fg-muted" />
              <div className="min-w-0">
                <p className="truncate text-sm text-fg">{g.name}</p>
                <p className="truncate text-xs text-fg-faint">
                  {g.cameraIds.length === 0
                    ? "No cameras"
                    : g.cameraIds
                        .map(nameOf)
                        .filter(Boolean)
                        .join(", ")}
                </p>
              </div>
              <div className="ml-auto flex shrink-0 items-center gap-1">
                <button className="btn-icon h-8 w-8" onClick={() => setEditing(g)} aria-label="Edit">
                  <Pencil className="h-4 w-4" />
                </button>
                <button className="btn-icon h-8 w-8 hover:text-rec" onClick={() => setConfirmDel(g)} aria-label="Delete">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
          {(groups ?? []).length === 0 && (
            <p className="rounded-lg border border-dashed border-line px-3 py-6 text-center text-xs text-fg-faint">
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
    </div>
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
    <div className="rounded-lg border border-line bg-ink-850 p-3">
      <label className="label">Group name</label>
      <input className="input" value={name} placeholder="e.g. Outdoors" onChange={(e) => setName(e.target.value)} />

      <label className="label mt-3">Cameras</label>
      <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
        {cameras.map((c) => {
          const on = sel.includes(c.id);
          return (
            <button
              key={c.id}
              onClick={() => toggle(c.id)}
              className={`flex items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left text-sm ${
                on ? "border-brand/50 bg-brand/10 text-fg" : "border-line text-fg-muted hover:bg-ink-700"
              }`}
            >
              <span className={`grid h-4 w-4 place-items-center rounded ${on ? "bg-brand text-white" : "border border-line-strong"}`}>
                {on && <Check className="h-3 w-3" />}
              </span>
              <Cctv className="h-3.5 w-3.5 shrink-0 text-fg-faint" />
              <span className="truncate">{c.name}</span>
            </button>
          );
        })}
      </div>

      <div className="mt-3 flex justify-end gap-2">
        <button className="btn-ghost h-9" onClick={onDone} disabled={busy}>
          Cancel
        </button>
        <button className="btn-primary h-9" onClick={save} disabled={busy || !name.trim()}>
          {busy ? "Saving…" : group ? "Save group" : "Create group"}
        </button>
      </div>
    </div>
  );
}
