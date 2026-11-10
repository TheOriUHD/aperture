import { useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "./ui/Modal";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { CredentialsCard } from "./CredentialsCard";
import { Switch } from "./ui/Switch";
import { Avatar } from "./ui/Layout";
import { useCameras } from "../hooks/useCameras";
import { useCreateUser, useUpdateUser, useDeleteUser, type ManagedUser } from "../hooks/useUsers";
import type { Role } from "../lib/types";

export function UserModal({
  open,
  user,
  currentUserId,
  onClose,
}: {
  open: boolean;
  user: ManagedUser | null; // null = add
  currentUserId?: string;
  onClose: () => void;
}) {
  const isEdit = !!user;
  const { data: cameras } = useCameras();
  const create = useCreateUser();
  const update = useUpdateUser();
  const del = useDeleteUser();

  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [password, setPassword] = useState("");
  const [disabled, setDisabled] = useState(false);
  const [camIds, setCamIds] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [confirmDel, setConfirmDel] = useState(false);
  const [card, setCard] = useState<{ email: string; password: string } | null>(null);

  useEffect(() => {
    if (!open) return;
    setError("");
    setConfirmDel(false);
    setCard(null);
    setPassword("");
    if (user) {
      setEmail(user.email);
      setName(user.name);
      setRole(user.role);
      setDisabled(user.disabled);
      setCamIds(user.cameraIds);
    } else {
      setEmail("");
      setName("");
      setRole("viewer");
      setDisabled(false);
      setCamIds([]);
    }
  }, [open, user]);

  const busy = create.isPending || update.isPending;
  const isSelf = isEdit && user?.id === currentUserId;

  function toggleCam(id: string) {
    setCamIds((prev) => (prev.includes(id) ? prev.filter((c) => c !== id) : [...prev, id]));
  }

  async function save() {
    if (!isEdit && !email.trim()) {
      setError("Email is required.");
      return;
    }
    if (isEdit && password && password.length < 8) {
      setError("Password must be at least 8 characters.");
      return;
    }
    try {
      if (user) {
        await update.mutateAsync({
          id: user.id,
          etag: user.etag, // If-Match: reject the save if this user changed since we loaded it
          name,
          role,
          disabled,
          password: password || undefined,
          cameraIds: role === "viewer" ? camIds : [],
        });
        onClose();
      } else {
        // No password sent → the server generates a temporary one and returns
        // it once so we can show the credentials card.
        const created = (await create.mutateAsync({
          email: email.trim(),
          name,
          role,
          cameraIds: role === "viewer" ? camIds : [],
        })) as ManagedUser & { tempPassword?: string };
        if (created.tempPassword) {
          setCard({ email: created.email, password: created.tempPassword });
        } else {
          onClose();
        }
      }
    } catch {
      setError("Could not save the user.");
    }
  }

  const camList = cameras ?? [];

  return (
    <>
      <Modal
        open={open && !card}
        onClose={onClose}
        title={isEdit ? "Edit user" : "Add user"}
        footer={
          <div className="flex items-center justify-between w-full">
            <div>
              {isEdit && !isSelf && (
                <button
                  className="btn-ghost border-danger/40 text-danger hover:border-danger/70 hover:bg-danger/10"
                  onClick={() => setConfirmDel(true)}
                >
                  <Trash2 className="h-4 w-4" />
                  Delete
                </button>
              )}
            </div>
            <div className="flex gap-2">
              <button className="btn-ghost" onClick={onClose}>
                Cancel
              </button>
              <button className="btn-primary" onClick={save} disabled={busy}>
                {busy ? "Saving…" : isEdit ? "Save changes" : "Add user"}
              </button>
            </div>
          </div>
        }
      >
        <div className="-my-5 divide-y divide-line">
          {/* Identity */}
          <div className="flex items-center gap-3 py-5">
            <Avatar name={name || email || "?"} className="!h-10 !w-10 rounded-md !text-sm" />
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold text-fg">{name || (isEdit ? email : "New user")}</p>
              <p className="truncate text-xs text-fg-muted">{isEdit ? email : "Someone who can sign in to this NVR"}</p>
            </div>
          </div>

          <div className="space-y-4 py-5">
            <div>
              <label className="label">Email</label>
              <input
                className="input"
                type="email"
                value={email}
                disabled={isEdit}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div>
              <label className="label">Name</label>
              <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className={isEdit ? "" : "sm:col-span-2"}>
                <label className="label">Role</label>
                <select className="input" value={role} onChange={(e) => setRole(e.target.value as Role)}>
                  <option value="viewer">Viewer</option>
                  <option value="admin">Administrator</option>
                </select>
              </div>
              {isEdit && (
                <div>
                  <label className="label">New password</label>
                  <input
                    className="input"
                    type="password"
                    placeholder="Leave blank to keep"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </div>
              )}
            </div>
            {!isEdit && (
              <p className="hint">
                A temporary password is generated and shown on a printable card. The user chooses their
                own password the first time they sign in, and can add an authenticator app.
              </p>
            )}
          </div>

          {role === "viewer" && (
            <div className="py-5">
              <div className="mb-1.5 flex items-baseline justify-between gap-3">
                <label className="label mb-0">Camera access</label>
                {camList.length > 0 && (
                  <span className="text-xs tabular-nums text-fg-faint">
                    {camIds.filter((id) => camList.some((c) => c.id === id)).length} of {camList.length}
                  </span>
                )}
              </div>
              <div className="max-h-52 divide-y divide-line overflow-auto rounded-md border border-line">
                {camList.map((c) => (
                  <label
                    key={c.id}
                    className="flex h-9 cursor-pointer items-center gap-2.5 px-3 text-sm text-fg/90 hover:bg-white/[0.03]"
                  >
                    <input
                      type="checkbox"
                      checked={camIds.includes(c.id)}
                      onChange={() => toggleCam(c.id)}
                    />
                    <span className="truncate">{c.name}</span>
                  </label>
                ))}
                {camList.length === 0 && (
                  <p className="px-3 py-2.5 text-xs text-fg-faint">No cameras to assign.</p>
                )}
              </div>
              <p className="hint mt-1.5">Viewers see live video and recordings of these cameras only.</p>
            </div>
          )}

          {isEdit && !isSelf && (
            <div className="flex items-center justify-between gap-4 py-5">
              <div>
                <span className="text-sm text-fg">Disabled</span>
                <p className="text-xs text-fg-faint">Prevent this account from signing in</p>
              </div>
              <Switch checked={disabled} onChange={setDisabled} label="Disabled" />
            </div>
          )}

          {error && (
            <div className="py-4">
              <p className="text-sm text-danger">{error}</p>
            </div>
          )}
        </div>
      </Modal>

      <ConfirmDialog
        open={confirmDel}
        danger
        busy={del.isPending}
        title="Delete user"
        message={`Permanently delete ${user?.name || user?.email}? This cannot be undone.`}
        confirmLabel="Delete user"
        onConfirm={async () => {
          if (!user) return;
          try {
            await del.mutateAsync(user.id);
            setConfirmDel(false);
            onClose();
          } catch {
            setError("Could not delete the user.");
            setConfirmDel(false);
          }
        }}
        onCancel={() => setConfirmDel(false)}
      />

      <CredentialsCard
        open={!!card}
        email={card?.email ?? ""}
        password={card?.password ?? ""}
        onClose={() => {
          setCard(null);
          onClose();
        }}
      />
    </>
  );
}
