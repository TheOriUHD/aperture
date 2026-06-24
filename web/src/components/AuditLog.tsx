import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ScrollText, ShieldCheck, ShieldAlert, RefreshCw } from "lucide-react";
import { api } from "../lib/api";
import { fmtDateTime } from "../lib/format";

interface AuditEntry {
  id: number;
  ts: number;
  actorEmail: string;
  action: string;
  target: string;
  detail: string;
  ip: string;
}

interface AuditResponse {
  entries: AuditEntry[];
  actions: string[];
}

function actionTone(action: string): string {
  if (action.includes("delete") || action.includes("purge") || action.includes("failed"))
    return "text-rec bg-rec/10";
  if (action.startsWith("auth.")) return "text-brand bg-brand/10";
  return "text-fg-muted bg-ink-700";
}

export function AuditLog() {
  const [action, setAction] = useState("");
  const [actor, setActor] = useState("");
  const [verify, setVerify] = useState<{ intact: boolean } | null>(null);

  const { data, isFetching, refetch } = useQuery({
    queryKey: ["audit", action, actor],
    queryFn: () => {
      const p = new URLSearchParams();
      if (action) p.set("action", action);
      if (actor) p.set("actor", actor);
      p.set("limit", "200");
      return api.get<AuditResponse>(`/api/system/audit?${p.toString()}`);
    },
  });

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg">
          <ScrollText className="h-4 w-4" />
          Activity log
        </h2>
        <div className="flex items-center gap-2">
          <button
            className="btn-subtle h-8 text-xs"
            onClick={async () => setVerify(await api.get<{ intact: boolean }>("/api/system/audit/verify"))}
          >
            {verify ? (
              verify.intact ? (
                <>
                  <ShieldCheck className="h-3.5 w-3.5 text-online" /> Intact
                </>
              ) : (
                <>
                  <ShieldAlert className="h-3.5 w-3.5 text-rec" /> Tampered
                </>
              )
            ) : (
              <>
                <ShieldCheck className="h-3.5 w-3.5" /> Verify chain
              </>
            )}
          </button>
          <button className="btn-icon h-8 w-8" onClick={() => refetch()} aria-label="Refresh">
            <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? "animate-spin" : ""}`} />
          </button>
        </div>
      </div>

      <p className="text-xs text-fg-faint mb-3 leading-relaxed">
        Tamper-evident, hash-chained record of logins, config changes, deletions, exports and
        footage access.
      </p>

      <div className="flex gap-2 mb-3">
        <select
          className="input h-9 w-44"
          value={action}
          onChange={(e) => setAction(e.target.value)}
        >
          <option value="">All actions</option>
          {(data?.actions ?? []).map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
        <input
          className="input h-9 flex-1"
          placeholder="Filter by user email…"
          value={actor}
          onChange={(e) => setActor(e.target.value)}
        />
      </div>

      <div className="max-h-96 overflow-auto rounded-lg border border-line">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-ink-850 text-fg-faint">
            <tr className="text-left">
              <th className="px-3 py-2 font-medium">When</th>
              <th className="px-3 py-2 font-medium">User</th>
              <th className="px-3 py-2 font-medium">Action</th>
              <th className="px-3 py-2 font-medium">Target</th>
              <th className="px-3 py-2 font-medium">Detail</th>
            </tr>
          </thead>
          <tbody>
            {(data?.entries ?? []).map((e) => (
              <tr key={e.id} className="border-t border-line/60">
                <td className="px-3 py-1.5 whitespace-nowrap text-fg-faint">{fmtDateTime(e.ts * 1000)}</td>
                <td className="px-3 py-1.5 whitespace-nowrap text-fg-muted">{e.actorEmail || "—"}</td>
                <td className="px-3 py-1.5 whitespace-nowrap">
                  <span className={`rounded px-1.5 py-0.5 font-mono ${actionTone(e.action)}`}>
                    {e.action}
                  </span>
                </td>
                <td className="px-3 py-1.5 whitespace-nowrap text-fg">{e.target || "—"}</td>
                <td className="px-3 py-1.5 text-fg-faint">{e.detail}</td>
              </tr>
            ))}
            {data && data.entries.length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-6 text-center text-fg-faint">
                  No activity yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
