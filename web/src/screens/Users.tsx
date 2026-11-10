import { useMemo, useState } from "react";
import { UserPlus, Users as UsersIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { UserModal } from "../components/UserModal";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { Avatar, CheckRow, SidebarPage, SidebarSearch, SidebarSection, StatusDot } from "../components/ui/Layout";
import { useAuth } from "../lib/auth";
import { useUsers, type ManagedUser } from "../hooks/useUsers";
import { useCameras } from "../hooks/useCameras";

// Users is the people with access to this NVR: who they are, their role, which
// cameras a viewer may see, and whether the account is active.
export function Users() {
  const { user: me } = useAuth();
  const { data: users, isLoading } = useUsers();
  const { data: cameras } = useCameras();
  const [search, setSearch] = useState("");
  const [roles, setRoles] = useState<Set<string>>(new Set());
  const [states, setStates] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<ManagedUser | null>(null);

  const all = users ?? [];
  const count = (pred: (u: ManagedUser) => boolean) => all.filter(pred).length;
  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((u) => {
      if (roles.size && !roles.has(u.role)) return false;
      if (states.size && !states.has(u.disabled ? "disabled" : "active")) return false;
      if (q && !`${u.name} ${u.email}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [all, search, roles, states]);

  const flip = (set: Set<string>, v: string, on: boolean) => {
    const n = new Set(set);
    if (on) n.add(v);
    else n.delete(v);
    return n;
  };
  const camName = (id: string) => cameras?.find((c) => c.id === id)?.name;

  const sidebar = (
    <div>
      <SidebarSearch value={search} onChange={setSearch} />
      <button
        className="btn-outline mt-3 w-full"
        onClick={() => {
          setEditing(null);
          setOpen(true);
        }}
      >
        <UserPlus className="h-4 w-4" />
        Add user
      </button>
      <div className="mt-2">
        <SidebarSection title="Status">
          <CheckRow label="Active" count={count((u) => !u.disabled)} checked={states.has("active")} onChange={(v) => setStates((s) => flip(s, "active", v))} />
          <CheckRow label="Disabled" count={count((u) => u.disabled)} checked={states.has("disabled")} onChange={(v) => setStates((s) => flip(s, "disabled", v))} />
        </SidebarSection>
        <SidebarSection title="Role">
          <CheckRow label="Admin" count={count((u) => u.role === "admin")} checked={roles.has("admin")} onChange={(v) => setRoles((s) => flip(s, "admin", v))} />
          <CheckRow label="Viewer" count={count((u) => u.role === "viewer")} checked={roles.has("viewer")} onChange={(v) => setRoles((s) => flip(s, "viewer", v))} />
        </SidebarSection>
      </div>
      <p className="mt-3 px-1 text-xs leading-relaxed text-fg-faint">
        Admins manage everything. Viewers see live video and recordings of the cameras assigned to them, and nothing else.
      </p>
    </div>
  );

  return (
    <>
      <TopBar title="Users" />
      <SidebarPage sidebar={sidebar} storageKey="users">
        <div className="min-h-0 flex-1 overflow-auto px-4 pb-6 lg:px-5">
          {isLoading ? (
            <div className="grid place-items-center py-20">
              <Spinner label="Loading users" />
            </div>
          ) : shown.length === 0 ? (
            <EmptyState
              icon={UsersIcon}
              title={all.length === 0 ? "No users yet" : "Nobody matches these filters"}
              hint={all.length === 0 ? "Add the people who should be able to sign in." : undefined}
            />
          ) : (
            <>
            <ul className="divide-y divide-line md:hidden">
              {shown.map((u) => (
                <li key={u.id}>
                  <button
                    className={`flex w-full items-center gap-3 py-3 text-left ${u.disabled ? "opacity-60" : ""}`}
                    onClick={() => {
                      setEditing(u);
                      setOpen(true);
                    }}
                  >
                    <Avatar name={u.name || u.email} className="h-8 w-8 text-xs" />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-fg">{u.name || u.email}</span>
                        {u.id === me?.id && <span className="pill-blue">You</span>}
                        <span className="pill-gray">{u.role === "admin" ? "Admin" : "Viewer"}</span>
                      </span>
                      <span className="block truncate text-xs text-fg-faint">{u.email}</span>
                    </span>
                    <StatusDot tone={u.disabled ? "off" : "ok"} />
                  </button>
                </li>
              ))}
            </ul>
            <table className="tbl tbl-hover hidden min-w-[720px] md:table">
              <thead className="sticky top-0 z-10 bg-ink-900">
                <tr>
                  <th>Name</th>
                  <th>Email</th>
                  <th>Cameras</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((u) => {
                  const names = (u.cameraIds ?? []).map(camName).filter(Boolean) as string[];
                  return (
                    <tr
                      key={u.id}
                      onClick={() => {
                        setEditing(u);
                        setOpen(true);
                      }}
                      className={u.disabled ? "opacity-60" : undefined}
                    >
                      <td>
                        <div className="flex items-center gap-2.5">
                          <Avatar name={u.name || u.email} className={u.disabled ? "bg-ink-650" : undefined} />
                          <span className="font-medium text-fg">{u.name || u.email}</span>
                          {u.id === me?.id && <span className="pill-blue">You</span>}
                          <span className="pill-gray">{u.role === "admin" ? "Admin" : "Viewer"}</span>
                        </div>
                      </td>
                      <td className="whitespace-nowrap">{u.email}</td>
                      <td className="max-w-[320px]">
                        {u.role === "admin" ? (
                          <span className="text-fg-faint">All cameras</span>
                        ) : names.length === 0 ? (
                          <span className="text-fg-faint">None</span>
                        ) : (
                          <span className="block truncate" title={names.join(", ")}>
                            {names.join(", ")}
                          </span>
                        )}
                      </td>
                      <td className="whitespace-nowrap">
                        <span className="inline-flex items-center gap-2">
                          <StatusDot tone={u.disabled ? "off" : "ok"} />
                          {u.disabled ? "Disabled" : "Active"}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            </>
          )}
        </div>
      </SidebarPage>
      <UserModal open={open} user={editing} currentUserId={me?.id} onClose={() => setOpen(false)} />
    </>
  );
}
