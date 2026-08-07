import {
  Globe,
  Cctv,
  FolderLock,
  Eye,
  PencilLine,
  ShieldCheck,
  KeyRound,
  type LucideIcon,
} from "lucide-react";

// Turns the server's plain-language consent lines into a grouped, scannable list.
// The consequential grants — network access ("can send your data") and all-camera
// scope — are visually elevated with a warning treatment; benign grants read
// calmly; the "cannot read your recordings…" reassurance renders as a positive
// footnote. The text itself is the server's wording, unchanged — we only classify
// and weight it, never paraphrase the actual grant.

type Kind = "network" | "cameras-all" | "data-write" | "data-read" | "cameras-some" | "storage" | "safe";

interface Classified {
  line: string;
  kind: Kind;
  risky: boolean;
  icon: LucideIcon;
}

function classify(line: string): Classified {
  const l = line.toLowerCase();
  if (l.includes("cannot read your recordings")) return { line, kind: "safe", risky: false, icon: ShieldCheck };
  if (l.startsWith("connect over the internet")) return { line, kind: "network", risky: true, icon: Globe };
  if (l.includes("for all your cameras")) return { line, kind: "cameras-all", risky: true, icon: Cctv };
  if (l.includes("only for the cameras")) return { line, kind: "cameras-some", risky: false, icon: Cctv };
  if (l.includes("keep its own settings")) return { line, kind: "storage", risky: false, icon: FolderLock };
  if (l.startsWith("read and change")) return { line, kind: "data-write", risky: false, icon: PencilLine };
  if (l.includes("alarm")) return { line, kind: "data-write", risky: false, icon: KeyRound };
  return { line, kind: "data-read", risky: false, icon: Eye };
}

/** Compact risk flags for a manager row's summary. */
export function permissionFlags(lines: string[]): { network: boolean; allCameras: boolean; write: boolean } {
  return {
    network: lines.some((l) => l.toLowerCase().startsWith("connect over the internet")),
    allCameras: lines.some((l) => l.toLowerCase().includes("for all your cameras")),
    write: lines.some((l) => l.toLowerCase().startsWith("read and change")),
  };
}

function Row({ item }: { item: Classified }) {
  const Icon = item.icon;
  if (item.kind === "safe") {
    return (
      <div className="flex items-center gap-2 px-1 pt-1 text-xs text-fg-muted">
        <ShieldCheck className="h-3.5 w-3.5 shrink-0 text-online" />
        <span>{item.line}</span>
      </div>
    );
  }
  const risky = item.risky;
  return (
    <div
      className={
        "flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm " +
        (risky
          ? "border-[#e0a54a]/35 bg-[#e0a54a]/10 text-fg"
          : "border-line bg-ink-850 text-fg")
      }
    >
      <Icon className={"mt-0.5 h-4 w-4 shrink-0 " + (risky ? "text-[#e6b45a]" : "text-fg-faint")} />
      <span className="leading-snug">{item.line}</span>
    </div>
  );
}

export function PermissionList({ lines }: { lines: string[] }) {
  if (!lines.length) return null;
  const items = lines.map(classify);
  // Consequential first, then benign grants, then the reassurance footnote.
  const risky = items.filter((i) => i.risky);
  const normal = items.filter((i) => !i.risky && i.kind !== "safe");
  const safe = items.filter((i) => i.kind === "safe");
  return (
    <div className="space-y-2">
      {risky.map((it, i) => (
        <Row key={"r" + i} item={it} />
      ))}
      {normal.map((it, i) => (
        <Row key={"n" + i} item={it} />
      ))}
      {safe.map((it, i) => (
        <Row key={"s" + i} item={it} />
      ))}
    </div>
  );
}
