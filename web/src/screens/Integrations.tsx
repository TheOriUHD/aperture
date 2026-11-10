import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import clsx from "clsx";
import { Cable, Code2, Home, KeyRound, LogIn, MonitorPlay, Radio, Share2, Webhook, type LucideIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { HomeAssistant } from "../components/HomeAssistant";
import { EufyBridge } from "../components/EufyBridge";
import { LiveViewCard } from "../components/LiveViewCard";
import { Interop } from "../components/Interop";
import { Webhooks } from "../components/Webhooks";
import { ApiTokens } from "../components/ApiTokens";
import { InteropEgress } from "../components/InteropEgress";
import { OidcSettings } from "../components/OidcSettings";
import { StatusDot } from "../components/ui/Layout";
import { useSystemStatus } from "../hooks/useSystem";

interface Item {
  id: string;
  label: string;
  hint: string;
  icon: LucideIcon;
  render: () => JSX.Element;
}

const ITEMS: Item[] = [
  { id: "home-assistant", label: "Home Assistant", hint: "Cameras, sensors and controls over MQTT", icon: Home, render: () => <HomeAssistant /> },
  { id: "eufy", label: "Eufy", hint: "Eufy cameras through a local bridge", icon: Radio, render: () => <EufyBridge /> },
  { id: "live-view", label: "Live View Card", hint: "Embed live video in dashboards", icon: MonitorPlay, render: () => <LiveViewCard /> },
  { id: "other-systems", label: "ONVIF & HomeKit", hint: "Let other systems watch your cameras", icon: Share2, render: () => <Interop /> },
  { id: "webhooks", label: "Webhooks", hint: "Send events to other services", icon: Webhook, render: () => <Webhooks /> },
  { id: "api", label: "API Tokens", hint: "Scoped keys for scripts and apps", icon: KeyRound, render: () => <ApiTokens /> },
  { id: "egress", label: "Interoperability", hint: "Advanced outbound connections", icon: Cable, render: () => <InteropEgress /> },
  { id: "sso", label: "Single Sign-On", hint: "Sign in with an OIDC provider", icon: LogIn, render: () => <OidcSettings /> },
];

// Integrations collects every way Aperture connects to other systems: a list on
// the left, the selected integration's settings on the right.
export function Integrations() {
  const [params, setParams] = useSearchParams();
  const requested = params.get("i") ?? ITEMS[0].id;
  const [active, setActive] = useState(requested);
  useEffect(() => setActive(requested), [requested]);
  const current = ITEMS.find((i) => i.id === active) ?? ITEMS[0];
  const { data: status } = useSystemStatus();
  const stripRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    stripRef.current?.querySelector<HTMLElement>("[data-active]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [active]);
  const select = (id: string) => {
    setActive(id);
    setParams({ i: id }, { replace: true });
  };

  return (
    <>
      <TopBar title="Integrations" />
      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        <nav className="hidden w-[280px] shrink-0 flex-col overflow-y-auto border-r border-line px-3 py-4 lg:flex">
          <p className="mb-2 px-3 text-xs font-semibold text-fg">Integrations</p>
          <div className="space-y-0.5">
            {ITEMS.map((it) => (
              <button
                key={it.id}
                onClick={() => select(it.id)}
                className={clsx(
                  "flex w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors",
                  active === it.id ? "bg-ink-700" : "hover:bg-white/[0.04]",
                )}
              >
                <it.icon className={clsx("h-[18px] w-[18px] shrink-0", active === it.id ? "text-brand" : "text-fg-muted")} strokeWidth={1.75} />
                <span className="min-w-0 flex-1">
                  <span className={clsx("block truncate text-sm", active === it.id ? "text-brand" : "text-fg/90")}>{it.label}</span>
                  <span className="block truncate text-xs text-fg-faint">{it.hint}</span>
                </span>
                {it.id === "home-assistant" && status?.homeAssistant.enabled && (
                  <StatusDot tone={status.homeAssistant.connected ? "ok" : "bad"} />
                )}
              </button>
            ))}
          </div>
          <div className="mt-auto flex items-start gap-2.5 px-3 pt-6 text-xs leading-relaxed text-fg-faint">
            <Code2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>
              The REST API is described at <span className="font-mono text-fg-muted">/api/v1/openapi.json</span>.
            </span>
          </div>
        </nav>
        <div className="shrink-0 border-b border-line lg:hidden">
          <div
            ref={stripRef}
            className="flex gap-1 overflow-x-auto px-3 py-2 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
          >
            {ITEMS.map((it) => (
              <button
                key={it.id}
                data-active={active === it.id ? "" : undefined}
                onClick={() => select(it.id)}
                className={clsx(
                  "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-sm",
                  active === it.id ? "bg-ink-700 text-brand" : "text-fg-muted",
                )}
              >
                <it.icon className="h-4 w-4" />
                {it.label}
              </button>
            ))}
          </div>
        </div>
        <div className="min-w-0 flex-1 overflow-y-auto">
          <div className="max-w-[960px] px-4 py-5 lg:px-8 lg:py-6">{current.render()}</div>
        </div>
      </div>
    </>
  );
}
