import { useMemo, useState } from "react";
import { KeyRound, Loader2, Plus, Trash2 } from "lucide-react";
import { Switch } from "../components/ui/Switch";
import { ApiError } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import type { ExtItem, GroupRow, SettingField, SettingsView } from "./types";
import { useExtSettings, useSaveExtSettings } from "./useExtensions";

// The extension's own settings, rendered as an app-native form on its row. Scalars,
// plus the shapes real configuration needs: a url (validated), a camera picker of the
// extension's granted cameras, and a repeatable group whose rows are a small set of
// fields (e.g. camera → light entity). Secret fields stay write-only.

type FormValue = string | GroupRow[];
type FormState = Record<string, FormValue>;

function scalarDefault(f: SettingField): string {
  if (f.default == null) return f.type === "boolean" ? "false" : "";
  if (typeof f.default === "boolean") return f.default ? "true" : "false";
  return String(f.default);
}

export function ExtSettingsForm({ ext }: { ext: ExtItem }) {
  const q = useExtSettings(ext.id, true);
  if (q.isLoading) {
    return (
      <div className="flex items-center gap-2 text-xs text-fg-muted">
        <Loader2 className="h-3.5 w-3.5 animate-spin text-brand" /> Loading settings…
      </div>
    );
  }
  if (q.isError || !q.data) {
    return (
      <div className="rounded-md border border-danger/30 bg-danger/[0.08] px-3 py-2.5 text-sm text-fg">Could not load settings.</div>
    );
  }
  // Re-mount on a data change (e.g. after a save flips a secret to "set") so the form
  // rebaselines cleanly.
  const key = JSON.stringify(q.data.values) + JSON.stringify(q.data.secretSet);
  return <SettingsBody key={key} ext={ext} view={q.data} />;
}

function SettingsBody({ ext, view }: { ext: ExtItem; view: SettingsView }) {
  const save = useSaveExtSettings();
  const { data: cams = [] } = useCameras();

  // The cameras a `camera` field may pick: the extension's granted set (all cameras
  // when the grant is empty).
  const cameraOpts = useMemo(() => {
    const grants = ext.cameraGrants ?? [];
    const list = grants.length ? cams.filter((c) => grants.includes(c.id)) : cams;
    return list.map((c) => ({ id: c.id, name: c.name || c.id }));
  }, [cams, ext.cameraGrants]);

  const baseline = useMemo<FormState>(() => {
    const b: FormState = {};
    for (const f of view.schema) {
      if (f.type === "group") b[f.key] = (view.values[f.key] as GroupRow[]) ?? [];
      else if (f.type === "secret") b[f.key] = "";
      else b[f.key] = (view.values[f.key] as string) ?? scalarDefault(f);
    }
    return b;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.schema, view.values]);
  const [form, setForm] = useState<FormState>(baseline);

  const setScalar = (k: string, v: string) => setForm((f) => ({ ...f, [k]: v }));
  const setGroup = (k: string, rows: GroupRow[]) => setForm((f) => ({ ...f, [k]: rows }));

  const dirty = JSON.stringify(form) !== JSON.stringify(baseline);

  const missing = view.schema.find((f) => {
    if (!f.required) return false;
    if (f.type === "group") return (form[f.key] as GroupRow[]).length === 0;
    if (f.type === "secret") return !view.secretSet[f.key] && (form[f.key] as string).trim() === "";
    return (form[f.key] as string).trim() === "";
  });
  const canSave = dirty && !missing && !save.isPending;

  const submit = () => {
    const values: Record<string, unknown> = {};
    for (const f of view.schema) {
      if (f.type === "group") {
        values[f.key] = (form[f.key] as GroupRow[]).map((row) =>
          Object.fromEntries((f.fields ?? []).map((sub) => [sub.key, subValue(sub, row[sub.key] ?? "")])),
        );
      } else if (f.type === "secret") {
        const v = form[f.key] as string;
        if (v.trim() !== "") values[f.key] = v; // omit → keep the stored secret
      } else {
        values[f.key] = subValue(f, form[f.key] as string);
      }
    }
    save.mutate({ id: ext.id, values });
  };

  const err = save.error instanceof ApiError ? save.error.message : save.isError ? "Could not save settings." : "";

  return (
    <div>
      <div className="mb-1">
        <p className="text-[13px] font-semibold text-fg">Settings</p>
        {ext.hasBackend && <p className="mt-0.5 text-xs text-fg-faint">Saving restarts the extension so it picks them up.</p>}
      </div>

      <div className="divide-y divide-line">
        {view.schema.map((f) =>
          f.type === "group" ? (
            <GroupField
              key={f.key}
              field={f}
              rows={form[f.key] as GroupRow[]}
              cameras={cameraOpts}
              onChange={(rows) => setGroup(f.key, rows)}
            />
          ) : (
            <ScalarField
              key={f.key}
              field={f}
              value={form[f.key] as string}
              secretSet={!!view.secretSet[f.key]}
              cameras={cameraOpts}
              onChange={(v) => setScalar(f.key, v)}
            />
          ),
        )}
      </div>

      {err && <p className="mt-3 text-sm text-danger">{err}</p>}

      <div className="mt-3 flex items-center justify-end gap-3">
        {save.isSuccess && !dirty && <p className="text-xs text-online">Saved.</p>}
        <button className="btn-subtle" disabled={!canSave} onClick={submit}>
          {save.isPending ? "Saving…" : "Save settings"}
        </button>
      </div>
    </div>
  );
}

// subValue converts a form string to the JSON value the API expects for a field type.
function subValue(f: SettingField, v: string): unknown {
  return f.type === "boolean" ? v === "true" : v;
}

type CamOpt = { id: string; name: string };

function ScalarField({
  field,
  value,
  secretSet,
  cameras,
  onChange,
  compact,
}: {
  field: SettingField;
  value: string;
  secretSet?: boolean;
  cameras: CamOpt[];
  onChange: (v: string) => void;
  compact?: boolean;
}) {
  const control = () => {
    switch (field.type) {
      case "boolean":
        return (
          <div className="mt-1">
            <Switch checked={value === "true"} onChange={(v) => onChange(v ? "true" : "false")} label={field.label} />
          </div>
        );
      case "select":
        return (
          <select className="input" value={value} onChange={(e) => onChange(e.target.value)}>
            {!field.required && <option value="">—</option>}
            {(field.options ?? []).map((o) => (
              <option key={o.value} value={o.value}>
                {o.label || o.value}
              </option>
            ))}
          </select>
        );
      case "camera": {
        const known = cameras.some((c) => c.id === value);
        return (
          <select className="input" value={value} onChange={(e) => onChange(e.target.value)}>
            <option value="">Select a camera…</option>
            {cameras.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
            {!known && value && <option value={value}>{value} (not granted)</option>}
          </select>
        );
      }
      default:
        return (
          <input
            className="input"
            type={field.type === "secret" ? "password" : field.type === "number" ? "number" : "text"}
            inputMode={field.type === "url" ? "url" : undefined}
            value={value}
            placeholder={
              field.type === "secret" && secretSet ? "•••••••• — set (leave blank to keep)" : field.placeholder || ""
            }
            autoComplete={field.type === "secret" ? "new-password" : "off"}
            onChange={(e) => onChange(e.target.value)}
          />
        );
    }
  };

  if (compact) return control();

  // A settings row: label on the left, control (and its help) on the right.
  return (
    <div className="grid gap-x-6 gap-y-1.5 py-3 md:grid-cols-[minmax(140px,200px)_1fr] md:items-start">
      <label className="text-sm text-fg-muted md:pt-1.5">
        {field.type === "secret" && <KeyRound className="mr-1.5 inline-block h-3.5 w-3.5 align-[-2px] text-fg-faint" />}
        {field.label}
        {field.required && <span className="text-danger"> *</span>}
      </label>
      <div className="min-w-0 max-w-md">
        {control()}
        {field.help && <p className="mt-1.5 text-xs leading-relaxed text-fg-faint">{field.help}</p>}
      </div>
    </div>
  );
}

// GroupField renders a repeatable list of rows, each row a set of the group's fields
// with add/remove — the shape for a camera → light-entity mapping and the like.
function GroupField({
  field,
  rows,
  cameras,
  onChange,
}: {
  field: SettingField;
  rows: GroupRow[];
  cameras: CamOpt[];
  onChange: (rows: GroupRow[]) => void;
}) {
  const subs = field.fields ?? [];
  const addRow = () => onChange([...rows, Object.fromEntries(subs.map((s) => [s.key, ""]))]);
  const removeRow = (i: number) => onChange(rows.filter((_, j) => j !== i));
  const setCell = (i: number, key: string, v: string) =>
    onChange(rows.map((r, j) => (j === i ? { ...r, [key]: v } : r)));

  return (
    <div className="grid gap-x-6 gap-y-1.5 py-3 md:grid-cols-[minmax(140px,200px)_1fr] md:items-start">
      <div className="md:pt-1.5">
        <label className="text-sm text-fg-muted">
          {field.label}
          {field.required && <span className="text-danger"> *</span>}
        </label>
        {field.help && <p className="mt-1 text-xs leading-relaxed text-fg-faint">{field.help}</p>}
      </div>

      <div className="min-w-0">
        {rows.length === 0 ? (
          <p className="py-1.5 text-xs text-fg-faint">None yet.</p>
        ) : (
          <div className="overflow-hidden rounded-md border border-line">
            {/* Column labels, then one line per row. */}
            <div className="flex gap-2 border-b border-line bg-white/[0.02] px-2.5 py-1.5">
              {subs.map((sub) => (
                <p key={sub.key} className="min-w-0 flex-1 truncate text-[11px] font-medium text-fg-muted">
                  {sub.label}
                </p>
              ))}
              <span className="w-8 shrink-0" />
            </div>
            <div className="divide-y divide-line">
              {rows.map((row, i) => (
                <div key={i} className="flex items-center gap-2 px-2.5 py-2">
                  {subs.map((sub) => (
                    <div key={sub.key} className="min-w-0 flex-1">
                      <ScalarField field={sub} value={row[sub.key] ?? ""} cameras={cameras} onChange={(v) => setCell(i, sub.key, v)} compact />
                    </div>
                  ))}
                  <button
                    type="button"
                    className="btn-icon shrink-0 text-fg-faint hover:text-danger"
                    aria-label="Remove"
                    onClick={() => removeRow(i)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        <button type="button" className="btn-ghost mt-2" onClick={addRow}>
          <Plus className="h-4 w-4" /> Add
        </button>
      </div>
    </div>
  );
}
