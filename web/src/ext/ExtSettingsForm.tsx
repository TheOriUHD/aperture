import { useMemo, useState } from "react";
import { KeyRound } from "lucide-react";
import { Switch } from "../components/ui/Switch";
import { ApiError } from "../lib/api";
import type { ExtItem, SettingField, SettingsView } from "./types";
import { useExtSettings, useSaveExtSettings } from "./useExtensions";

// The extension's own settings, rendered as an app-native form on its row (not a
// generic key/value editor). Values live in the extension's private storage and are
// read back by both SDK transports. Secret fields are write-only: a set secret shows
// a masked "set" placeholder and is replaced by typing — its value is never loaded.

function defaultToString(f: SettingField): string {
  if (f.default == null) return f.type === "boolean" ? "false" : "";
  if (typeof f.default === "boolean") return f.default ? "true" : "false";
  return String(f.default);
}

export function ExtSettingsForm({ ext }: { ext: ExtItem }) {
  const q = useExtSettings(ext.id, true);
  if (q.isLoading) {
    return (
      <div className="rounded-lg border border-line bg-ink-900/40 px-3 py-3 text-xs text-fg-muted">
        Loading settings…
      </div>
    );
  }
  if (q.isError || !q.data) {
    return (
      <div className="rounded-lg border border-rec/30 bg-rec/10 px-3 py-2.5 text-sm text-fg">
        Could not load settings.
      </div>
    );
  }
  return <SettingsFields ext={ext} view={q.data} />;
}

function SettingsFields({ ext, view }: { ext: ExtItem; view: SettingsView }) {
  const save = useSaveExtSettings();
  const baseline = useMemo(() => {
    const b: Record<string, string> = {};
    for (const f of view.schema) {
      b[f.key] = f.type === "secret" ? "" : (view.values[f.key] ?? defaultToString(f));
    }
    return b;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.schema, view.values]);
  const [form, setForm] = useState<Record<string, string>>(baseline);

  const set = (k: string, v: string) => setForm((f) => ({ ...f, [k]: v }));
  const dirty = view.schema.some((f) =>
    f.type === "secret" ? form[f.key].trim() !== "" : form[f.key] !== baseline[f.key],
  );

  // A required non-secret must have a value; a required secret must be set already or
  // typed now. This mirrors the server's validation so Save reflects it.
  const missingRequired = view.schema.find((f) => {
    if (!f.required) return false;
    if (f.type === "secret") return !view.secretSet[f.key] && form[f.key].trim() === "";
    return form[f.key].trim() === "";
  });
  const canSave = dirty && !missingRequired && !save.isPending;

  const submit = () => {
    const values: Record<string, unknown> = {};
    for (const f of view.schema) {
      const v = form[f.key];
      if (f.type === "secret") {
        if (v.trim() !== "") values[f.key] = v; // omit → keep the stored secret
      } else if (f.type === "boolean") {
        values[f.key] = v === "true";
      } else if (f.type === "number") {
        values[f.key] = v.trim() === "" ? "" : Number.isNaN(Number(v)) ? v : Number(v);
      } else {
        values[f.key] = v;
      }
    }
    save.mutate({ id: ext.id, values });
  };

  const err = save.error instanceof ApiError ? save.error.message : save.isError ? "Could not save settings." : "";

  return (
    <div className="rounded-lg border border-line bg-ink-900/40 p-3">
      <div className="mb-3 flex items-center justify-between gap-2">
        <p className="text-sm text-fg">Settings</p>
        {ext.hasBackend && (
          <span className="text-[11px] text-fg-faint">Saving restarts the extension so it picks them up.</span>
        )}
      </div>

      <div className="space-y-3">
        {view.schema.map((f) => (
          <Field
            key={f.key}
            field={f}
            value={form[f.key]}
            secretSet={!!view.secretSet[f.key]}
            onChange={(v) => set(f.key, v)}
          />
        ))}
      </div>

      {err && <p className="mt-3 text-sm text-rec">{err}</p>}
      {save.isSuccess && !dirty && <p className="mt-3 text-xs text-online">Saved.</p>}

      <div className="mt-3 flex justify-end">
        <button className="btn-subtle h-8" disabled={!canSave} onClick={submit}>
          {save.isPending ? "Saving…" : "Save settings"}
        </button>
      </div>
    </div>
  );
}

function Field({
  field,
  value,
  secretSet,
  onChange,
}: {
  field: SettingField;
  value: string;
  secretSet: boolean;
  onChange: (v: string) => void;
}) {
  const req = field.required ? <span className="text-rec"> *</span> : null;
  return (
    <div>
      <label className="label flex items-center gap-1.5">
        {field.type === "secret" && <KeyRound className="h-3 w-3 text-fg-faint" />}
        {field.label}
        {req}
      </label>

      {field.type === "boolean" ? (
        <div className="mt-1">
          <Switch checked={value === "true"} onChange={(v) => onChange(v ? "true" : "false")} label={field.label} />
        </div>
      ) : field.type === "select" ? (
        <select className="input" value={value} onChange={(e) => onChange(e.target.value)}>
          {!field.required && <option value="">—</option>}
          {(field.options ?? []).map((o) => (
            <option key={o.value} value={o.value}>
              {o.label || o.value}
            </option>
          ))}
        </select>
      ) : (
        <input
          className="input"
          type={field.type === "secret" ? "password" : field.type === "number" ? "number" : "text"}
          value={value}
          placeholder={
            field.type === "secret" && secretSet
              ? "•••••••• — set (leave blank to keep)"
              : field.placeholder || ""
          }
          autoComplete={field.type === "secret" ? "new-password" : "off"}
          onChange={(e) => onChange(e.target.value)}
        />
      )}

      {field.help && <p className="mt-1 text-[11px] leading-relaxed text-fg-faint">{field.help}</p>}
    </div>
  );
}
