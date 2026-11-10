import { useEffect, useState } from "react";
import { KeyRound } from "lucide-react";
import { Switch } from "./ui/Switch";
import { SettingsHeading, SettingsRow } from "./ui/Layout";
import { CodeValue, CopyIconButton, IntegrationHeader, SectionTitle, StatusPill, ToggleRow } from "./IntegrationParts";
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
    <div>
      <IntegrationHeader
        icon={KeyRound}
        title="Single sign-on (OIDC)"
        status={oidc?.enabled && <StatusPill tone="ok">Enabled</StatusPill>}
      >
        Let users sign in with an OpenID Connect provider (Authentik, Authelia, Keycloak, Google,
        Entra ID…). Existing accounts are matched by email; new sign-ins can be auto-provisioned as
        viewers. Local accounts keep working alongside SSO.
      </IntegrationHeader>

      <SettingsHeading>Provider</SettingsHeading>
      <div className="divide-y divide-line">
        <ToggleRow label="Enable SSO login" hint="Shows a sign-in button on the login page">
          <Switch checked={enabled} onChange={setEnabled} label="Enable SSO" />
        </ToggleRow>
        <SettingsRow label="Button label">
          <input
            className="input max-w-md"
            placeholder="Single sign-on"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow
          label="Issuer URL"
          hint={
            <>
              The provider&apos;s base URL — its <code className="text-fg-muted">.well-known/openid-configuration</code> must be reachable from Aperture.
            </>
          }
        >
          <input
            className="input max-w-md font-mono text-xs"
            placeholder="https://auth.example.com/application/o/aperture/"
            value={issuer}
            onChange={(e) => setIssuer(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="Client ID">
          <input className="input max-w-md" value={clientId} onChange={(e) => setClientId(e.target.value)} />
        </SettingsRow>
        <SettingsRow label="Client secret">
          <input
            className="input max-w-md"
            type="password"
            placeholder={oidc?.hasSecret ? "Leave blank to keep" : ""}
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="Restrict to email domain (optional)">
          <input
            className="input max-w-md"
            placeholder="example.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
          />
        </SettingsRow>
        <ToggleRow label="Auto-create accounts" hint="First-time SSO users become viewers automatically">
          <Switch checked={autoCreate} onChange={setAutoCreate} label="Auto-create accounts" />
        </ToggleRow>
        <SettingsRow label="">
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
        </SettingsRow>
      </div>

      {oidc?.redirectUrl && (
        <>
          <SectionTitle
            description={
              <>
                Register this callback URL in your provider (set <code className="text-fg-muted">APERTURE_PUBLIC_URL</code> so it matches your real address):
              </>
            }
          >
            Redirect URI
          </SectionTitle>
          <div className="flex max-w-2xl items-center gap-1">
            <CodeValue className="flex-1">{oidc.redirectUrl}</CodeValue>
            <CopyIconButton copied={copied} onClick={copyRedirect} label="Copy redirect URL" />
          </div>
        </>
      )}
    </div>
  );
}
