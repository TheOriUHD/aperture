import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Check, ExternalLink, Loader2, MonitorCog, RefreshCw } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { CopyIconButton } from "./IntegrationParts";

type Step = {
  id: string;
  title: string;
  state: "done" | "todo" | "warn" | "later";
  detail?: string;
  commands?: string[];
  note?: string;
  link?: string;
};
type GPUSetup = {
  gpu?: { pci: string; deviceId: string; name: string; family: string; openModules: boolean; openRequired: boolean; minDriver?: number };
  os: { id: string; version: string; codename?: string; pretty: string; repoDist?: string; supported: boolean };
  kernel: string;
  vm?: string;
  secureBoot?: string;
  driver?: string;
  driverOpen?: boolean;
  ready: boolean;
  smi?: string;
  sudo: boolean;
  steps: Step[];
};

// GPUGuide walks through getting an NVIDIA GPU working on this machine.
// Aperture installs no driver itself: the server looks at the system (the
// distribution, kernel, Secure Boot, the card and what's already in place) and
// the guide shows only the steps still to do, with the exact commands for this
// machine. It re-checks every few seconds, so steps tick off as they're done.
export function GPUGuide({ onReady, readyLabel = "Use the GPU" }: { onReady?: () => void; readyLabel?: string }) {
  const { data: s, refetch, isFetching } = useQuery({
    queryKey: ["ai-gpu"],
    queryFn: () => api.get<GPUSetup>("/api/system/ai/gpu"),
    refetchInterval: (q) => (q.state.data?.ready ? false : 15_000),
  });

  if (!s) {
    return (
      <div className="flex items-center gap-2 py-4 text-sm text-fg-muted">
        <Loader2 className="h-4 w-4 animate-spin" /> Checking this machine…
      </div>
    );
  }

  const done = s.steps.filter((x) => x.state === "done").length;
  const facts = [
    s.os.pretty,
    `kernel ${s.kernel}`,
    s.secureBoot ? `Secure Boot ${s.secureBoot}` : null,
    s.vm ? `virtual machine (${s.vm})` : null,
  ].filter(Boolean);

  return (
    <div className="card p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-brand/12 text-brand">
            <MonitorCog className="h-[18px] w-[18px]" />
          </span>
          <div className="min-w-0">
            <p className="text-sm font-semibold text-fg">
              {s.ready ? `${s.gpu?.name ?? "The GPU"} is ready` : `Set up ${s.gpu?.name ?? "an NVIDIA GPU"}`}
            </p>
            <p className="mt-0.5 text-xs leading-relaxed text-fg-faint">
              {s.gpu?.family ? `${s.gpu.family} · ` : ""}
              {facts.join(" · ")}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <span className="text-xs tabular-nums text-fg-muted">
            {done} of {s.steps.length} done
          </span>
          <button className="btn-ghost" onClick={() => void refetch()} disabled={isFetching}>
            <RefreshCw className={clsx("h-4 w-4", isFetching && "animate-spin")} /> Check again
          </button>
        </div>
      </div>

      {!s.ready && (
        <p className="mt-4 rounded-md bg-white/[0.03] px-3.5 py-2.5 text-xs leading-relaxed text-fg-muted">
          Aperture doesn&apos;t install drivers itself. Run the commands below in a terminal on this machine (over SSH
          is fine), top to bottom.{" "}
          {s.sudo
            ? "Those starting with sudo ask for your password."
            : "This machine has no sudo, so run them as root."}{" "}
          This page notices each step as it&apos;s done.
        </p>
      )}

      <ol className="mt-4">
        {s.steps.map((st, i) => (
          <StepRow key={st.id} step={st} n={i + 1} last={i === s.steps.length - 1} root={!s.sudo} />
        ))}
      </ol>

      {s.ready && onReady && (
        <div className="mt-2 flex flex-wrap items-center gap-3 rounded-md bg-online/[0.08] px-3.5 py-3">
          <Check className="h-4 w-4 shrink-0 text-online" />
          <p className="min-w-0 flex-1 text-xs text-fg">The driver works. Object recognition can switch to the GPU now.</p>
          <button className="btn-primary" onClick={onReady}>
            {readyLabel}
          </button>
        </div>
      )}
    </div>
  );
}

function StepRow({ step, n, last, root }: { step: Step; n: number; last: boolean; root: boolean }) {
  const open = step.state === "todo" || step.state === "warn";
  return (
    <li className="relative flex gap-3.5 pb-5">
      {!last && <span className="absolute left-[11px] top-7 h-[calc(100%-1.5rem)] w-px bg-line-strong" />}
      <span
        className={clsx(
          "relative z-[1] grid h-6 w-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold",
          step.state === "done" && "bg-online/15 text-online",
          step.state === "todo" && "bg-brand text-white",
          step.state === "warn" && "bg-warn/15 text-warn",
          step.state === "later" && "border border-line-strong text-fg-faint",
        )}
      >
        {step.state === "done" ? <Check className="h-3.5 w-3.5" /> : step.state === "warn" ? <AlertTriangle className="h-3.5 w-3.5" /> : n}
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <p className={clsx("text-[13px] font-medium", step.state === "done" || step.state === "later" ? "text-fg-muted" : "text-fg")}>
          {step.title}
        </p>
        {step.detail && <p className="mt-0.5 text-xs leading-relaxed text-fg-faint">{step.detail}</p>}
        {open && step.commands && step.commands.length > 0 && (
          <div className="mt-2.5 space-y-1.5">
            {step.commands.map((c) => (
              <Command key={c} text={c} root={root} />
            ))}
          </div>
        )}
        {step.note && (open || step.state === "later") && (
          <p className="mt-2.5 rounded-md border border-line px-3 py-2 text-xs leading-relaxed text-fg-muted">{step.note}</p>
        )}
        {step.link && open && (
          <a href={step.link} target="_blank" rel="noreferrer" className="link mt-2 inline-flex items-center gap-1 text-xs">
            More in the guide <ExternalLink className="h-3 w-3" />
          </a>
        )}
      </div>
    </li>
  );
}

function Command({ text, root }: { text: string; root: boolean }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Clipboard needs a secure page; select the text instead.
      const range = document.createRange();
      const el = document.getElementById(`cmd-${hash(text)}`);
      if (el) {
        range.selectNodeContents(el);
        window.getSelection()?.removeAllRanges();
        window.getSelection()?.addRange(range);
      }
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }
  return (
    <div className="flex items-start gap-1.5">
      <code
        id={`cmd-${hash(text)}`}
        className="block min-w-0 flex-1 overflow-x-auto whitespace-pre rounded-md border border-white/[0.07] bg-black/30 px-3 py-[7px] font-mono text-xs leading-[18px] text-fg/90"
      >
        <span className="select-none text-fg-faint">{root ? "# " : "$ "}</span>
        {text}
      </code>
      <CopyIconButton copied={copied} onClick={copy} label="Copy command" />
    </div>
  );
}

function hash(s: string): string {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return Math.abs(h).toString(36);
}
