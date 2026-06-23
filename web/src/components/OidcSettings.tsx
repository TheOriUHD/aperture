import { useEffect, useState } from "react";
import { Copy, Check, KeyRound } from "lucide-react";
import { Switch } from "./ui/Switch";
import { useOIDC, useSetOIDC } from "../hooks/useSystem";

export function OidcSettings() {
  const { data: oidc } = useOIDC();
  const setOIDC = useSetOIDC();

  const [enabled, setEnabled] = useState(false);
  const [name, setName] = useState("Single sign-on");
  const [issuer, setIssuer] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [domain, setDomain] = useState("");
  const [autoCreate, setAutoCreate] = useState(false);
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!oidc) return;
    setEnabled(oidc.enabled);
    setName(oidc.name || "Single sign-on");
    setIssuer(oidc.issuer);
    setClientId(oidc.clientId);
    setDomain(oidc.domain);
    setAutoCreate(oidc.autoCreate);
  }, [oidc]);

  async function copyRedirect() {
    if (!oidc) return;
    try {
      await navigator.clipboard.writeText(oidc.redirectUrl);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg">
          <KeyRound className="h-4 w-4" />
          Single sign-on (OIDC)
        </h2>
        {oidc?.enabled && (
          <span className="pill pill-online">
            <span className="h-1.5 w-1.5 rounded-full bg-online" />
            Enabled
          </span>
        )}
      </div>

      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        Let users sign in with an OpenID Connect provider (Authentik, Authelia, Keycloak, Google,
        Entra ID…). Existing accounts are matched by email; new sign-ins can be auto-provisioned as
        viewers. Local accounts keep working alongside SSO.
      </p>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <span className="text-sm text-fg">Enable SSO login</span>
            <p className="text-xs text-fg-faint">Shows a sign-in button on the login page</p>
          </div>
          <Switch checked={enabled} onChange={setEnabled} label="Enable SSO" />
        </div>

        <div>
          <label className="label">Button label</label>
          <input
            className="input"
            placeholder="Single sign-on"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div>
          <label className="label">Issuer URL</label>
          <input
            className="input font-mono text-xs"
            placeholder="https://auth.example.com/application/o/aperture/"
            value={issuer}
            onChange={(e) => setIssuer(e.target.value)}
          />
          <p className="text-xs text-fg-faint mt-1.5">
            The provider&apos;s base URL — its <code className="text-fg-muted">.well-known/openid-configuration</code> must be reachable from Aperture.
          </p>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Client ID</label>
            <input className="input" value={clientId} onChange={(e) => setClientId(e.target.value)} />
          </div>
          <div>
            <label className="label">Client secret</label>
            <input
              className="input"
              type="password"
              placeholder={oidc?.hasSecret ? "Leave blank to keep" : ""}
              value={clientSecret}
              onChange={(e) => setClientSecret(e.target.value)}
            />
          </div>
        </div>

        <div>
          <label className="label">Restrict to email domain (optional)</label>
          <input
            className="input"
            placeholder="example.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
          />
        </div>

        <div className="flex items-center justify-between">
          <div>
            <span className="text-sm text-fg">Auto-create accounts</span>
            <p className="text-xs text-fg-faint">First-time SSO users become viewers automatically</p>
          </div>
          <Switch checked={autoCreate} onChange={setAutoCreate} label="Auto-create accounts" />
        </div>

        <div className="flex items-center gap-3">
          <button
            className="btn-primary"
            disabled={setOIDC.isPending}
            onClick={async () => {
              await setOIDC.mutateAsync({
                enabled,
                issuer,
                clientId,
                clientSecret: clientSecret || undefined,
                domain,
                autoCreate,
                name,
              });
              setClientSecret("");
              setSaved(true);
              setTimeout(() => setSaved(false), 1500);
            }}
          >
            {setOIDC.isPending ? "Saving…" : "Save"}
          </button>
          {saved && <span className="text-xs text-online">Saved.</span>}
        </div>
      </div>

      {oidc?.redirectUrl && (
        <div className="border-t border-line mt-5 pt-4">
          <p className="text-xs font-medium text-fg-muted mb-1">Redirect URI</p>
          <p className="text-xs text-fg-faint mb-2.5">
            Register this callback URL in your provider (set <code className="text-fg-muted">APERTURE_PUBLIC_URL</code> so it matches your real address):
          </p>
          <div className="flex items-center gap-2 text-xs">
            <code className="flex-1 truncate rounded bg-ink-850 px-2 py-1 font-mono text-fg-muted">
              {oidc.redirectUrl}
            </code>
            <button className="btn-icon h-7 w-7" onClick={copyRedirect} aria-label="Copy redirect URL">
              {copied ? <Check className="h-3.5 w-3.5 text-online" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
