import { useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "./ui/Modal";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { CredentialsCard } from "./CredentialsCard";
import { Switch } from "./ui/Switch";
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
                  className="btn-ghost !text-rec !border-rec/30 hover:!bg-rec/10"
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
        <div className="space-y-4">
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
          <div className="grid grid-cols-2 gap-3">
            <div className={isEdit ? "" : "col-span-2"}>
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
            <p className="text-xs text-fg-faint -mt-1">
              A temporary password is generated and shown on a printable card. The user chooses their
              own password the first time they sign in, and can add an authenticator app.
            </p>
          )}

          {role === "viewer" && (
            <div>
              <label className="label">Camera access</label>
              <div className="rounded-md border border-line-strong divide-y divide-line max-h-44 overflow-auto">
                {(cameras ?? []).map((c) => (
                  <label
                    key={c.id}
                    className="flex items-center gap-2.5 px-3 py-2 text-sm cursor-pointer hover:bg-ink-700"
                  >
                    <input
                      type="checkbox"
                      className="accent-brand"
                      checked={camIds.includes(c.id)}
                      onChange={() => toggleCam(c.id)}
                    />
                    <span className="text-fg">{c.name}</span>
                  </label>
                ))}
                {(cameras ?? []).length === 0 && (
                  <p className="px-3 py-2 text-xs text-fg-faint">No cameras to assign.</p>
                )}
              </div>
            </div>
          )}

          {isEdit && !isSelf && (
            <div className="flex items-center justify-between pt-1">
              <div>
                <span className="text-sm text-fg">Disabled</span>
                <p className="text-xs text-fg-faint">Prevent this account from signing in</p>
              </div>
              <Switch checked={disabled} onChange={setDisabled} label="Disabled" />
            </div>
          )}

          {error && <p className="text-sm text-rec">{error}</p>}
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
