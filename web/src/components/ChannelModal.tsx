import { useState } from "react";
import { Modal } from "./ui/Modal";
import { useCreateChannel } from "../hooks/useNotify";

interface Field {
  key: string;
  label: string;
  ph?: string;
  optional?: boolean;
}

const TYPES: { type: string; label: string; blurb: string; fields: Field[] }[] = [
  { type: "webpush", label: "Web Push", blurb: "Push to this account’s installed devices/browsers.", fields: [] },
  {
    type: "ntfy",
    label: "ntfy",
    blurb: "Self-hostable push — the homelab favorite.",
    fields: [
      { key: "server", label: "Server", ph: "https://ntfy.sh" },
      { key: "topic", label: "Topic" },
      { key: "token", label: "Access token", optional: true },
    ],
  },
  {
    type: "telegram",
    label: "Telegram",
    blurb: "Send to a chat via a bot.",
    fields: [
      { key: "token", label: "Bot token" },
      { key: "chatId", label: "Chat ID" },
    ],
  },
  { type: "discord", label: "Discord", blurb: "Post to a channel webhook.", fields: [{ key: "webhookUrl", label: "Webhook URL" }] },
  { type: "webhook", label: "Webhook", blurb: "POST JSON to any URL (n8n, Node-RED, …).", fields: [{ key: "url", label: "POST URL" }] },
  {
    type: "email",
    label: "Email (SMTP)",
    blurb: "Send an email with a thumbnail + link.",
    fields: [
      { key: "host", label: "SMTP host" },
      { key: "port", label: "Port", ph: "587" },
      { key: "username", label: "Username", optional: true },
      { key: "password", label: "Password", optional: true },
      { key: "from", label: "From address" },
      { key: "to", label: "To address(es)" },
    ],
  },
];

export function ChannelModal({ onClose }: { onClose: () => void }) {
  const create = useCreateChannel();
  const [type, setType] = useState("ntfy");
  const [name, setName] = useState("");
  const [cfg, setCfg] = useState<Record<string, string>>({});
  const [err, setErr] = useState("");

  const def = TYPES.find((t) => t.type === type)!;

  return (
    <Modal
      open
      onClose={onClose}
      title="Add notification channel"
      footer={
        <>
          <button className="btn-subtle" onClick={onClose}>
            Cancel
          </button>
          <button
            className="btn-primary"
            disabled={create.isPending}
            onClick={async () => {
              setErr("");
              const missing = def.fields.find((f) => !f.optional && !cfg[f.key]?.trim());
              if (missing) {
                setErr(`${missing.label} is required.`);
                return;
              }
              try {
                await create.mutateAsync({ type, name: name.trim() || def.label, config: cfg });
                onClose();
              } catch (e) {
                setErr((e as Error).message || "Could not create channel");
              }
            }}
          >
            {create.isPending ? "Adding…" : "Add channel"}
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div>
          <label className="label">Type</label>
          <select
            className="input"
            value={type}
            onChange={(e) => {
              setType(e.target.value);
              setCfg({});
            }}
          >
            {TYPES.map((t) => (
              <option key={t.type} value={t.type}>
                {t.label}
              </option>
            ))}
          </select>
          <p className="text-xs text-fg-faint mt-1.5">{def.blurb}</p>
        </div>

        <div>
          <label className="label">Name</label>
          <input
            className="input"
            placeholder={def.label}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        {def.fields.map((f) => (
          <div key={f.key}>
            <label className="label">
              {f.label}
              {f.optional && <span className="text-fg-faint"> (optional)</span>}
            </label>
            <input
              className="input font-mono text-xs"
              placeholder={f.ph}
              value={cfg[f.key] ?? ""}
              onChange={(e) => setCfg((c) => ({ ...c, [f.key]: e.target.value }))}
            />
          </div>
        ))}

        {type === "webpush" && (
          <p className="text-xs text-fg-faint leading-relaxed">
            After adding this, turn on <span className="text-fg-muted">“Push on this device”</span> at
            the top of Notifications on each device you want alerts on.
          </p>
        )}
        {err && <p className="text-xs text-rec">{err}</p>}
      </div>
    </Modal>
  );
}
