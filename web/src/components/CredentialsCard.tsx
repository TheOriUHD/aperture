import { useState } from "react";
import { AlertTriangle, Download, Printer, Copy, Check } from "lucide-react";
import { Modal } from "./ui/Modal";

// cardSvg renders the credentials card as a self-contained SVG string, used for
// both on-screen preview and PNG/print export (one source of truth). Its colours
// are literal because the SVG leaves the app (PNG, print), but they mirror the
// theme: ink-800 card, line-strong hairlines, brand accent, fg / fg-muted text.
function cardSvg(loginUrl: string, email: string, password: string): string {
  const esc = (s: string) =>
    s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return `<svg xmlns="http://www.w3.org/2000/svg" width="760" height="460" viewBox="0 0 760 460" font-family="Inter, system-ui, sans-serif">
  <defs><clipPath id="aperture-cred-card"><rect x="1" y="1" width="758" height="458" rx="12"/></clipPath></defs>
  <rect x="1" y="1" width="758" height="458" rx="12" fill="#1b1e21" stroke="#2f3236" stroke-width="1.5"/>
  <rect x="1" y="1" width="758" height="6" fill="#006fff" clip-path="url(#aperture-cred-card)"/>
  <g transform="translate(48,56)">
    <circle cx="13" cy="13" r="13" fill="#006fff"/>
    <circle cx="13" cy="13" r="5" fill="#1b1e21"/>
    <text x="40" y="11" fill="#f2f4f5" font-size="17" font-weight="600" letter-spacing="2">APERTURE NVR</text>
    <text x="40" y="30" fill="#a5a8ac" font-size="12.5" letter-spacing="0.5">Account access card</text>
  </g>
  <line x1="48" y1="112" x2="712" y2="112" stroke="#2f3236" stroke-width="1"/>
  <g transform="translate(48,150)">
    <text x="0" y="0" fill="#a5a8ac" font-size="12" letter-spacing="1.5">SIGN-IN ADDRESS</text>
    <text x="0" y="26" fill="#f2f4f5" font-size="19">${esc(loginUrl)}</text>
  </g>
  <g transform="translate(48,222)">
    <text x="0" y="0" fill="#a5a8ac" font-size="12" letter-spacing="1.5">EMAIL</text>
    <text x="0" y="26" fill="#f2f4f5" font-size="19">${esc(email)}</text>
  </g>
  <g transform="translate(48,290)">
    <text x="0" y="0" fill="#a5a8ac" font-size="12" letter-spacing="1.5">TEMPORARY PASSWORD</text>
    <rect x="0" y="14" width="664" height="52" rx="8" fill="#111314" stroke="#2f3236"/>
    <text x="22" y="48" fill="#4797ff" font-size="26" font-weight="600" font-family="ui-monospace, SFMono-Regular, Menlo, monospace" letter-spacing="2">${esc(password)}</text>
  </g>
  <text x="48" y="416" fill="#a5a8ac" font-size="12.5">You'll be asked to choose your own password the first time you sign in.</text>
</svg>`;
}

export function CredentialsCard({
  open,
  email,
  password,
  onClose,
}: {
  open: boolean;
  email: string;
  password: string;
  onClose: () => void;
}) {
  const loginUrl = window.location.origin;
  const svg = cardSvg(loginUrl, email, password);
  const [copied, setCopied] = useState(false);

  function downloadPng() {
    const blob = new Blob([svg], { type: "image/svg+xml" });
    const url = URL.createObjectURL(blob);
    const img = new Image();
    img.onload = () => {
      const scale = 2;
      const canvas = document.createElement("canvas");
      canvas.width = 760 * scale;
      canvas.height = 460 * scale;
      const ctx = canvas.getContext("2d")!;
      ctx.scale(scale, scale);
      ctx.drawImage(img, 0, 0);
      URL.revokeObjectURL(url);
      canvas.toBlob((png) => {
        if (!png) return;
        const a = document.createElement("a");
        a.href = URL.createObjectURL(png);
        a.download = `aperture-credentials-${email.replace(/[^a-z0-9]/gi, "_")}.png`;
        a.click();
        URL.revokeObjectURL(a.href);
      }, "image/png");
    };
    img.src = url;
  }

  function print() {
    const w = window.open("", "_blank", "width=820,height=560");
    if (!w) return;
    w.document.write(
      `<html><head><title>Aperture credentials</title></head><body style="margin:0;display:grid;place-items:center;height:100vh;background:#111314">${svg}</body></html>`,
    );
    w.document.close();
    w.focus();
    setTimeout(() => w.print(), 250);
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(password);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable */
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Account created"
      size="lg"
      footer={
        <div className="flex w-full items-center justify-between gap-4">
          <p className="flex max-w-[20rem] items-start gap-1.5 text-xs text-warn">
            <AlertTriangle className="mt-px h-3.5 w-3.5 shrink-0" />
            This password is shown only once — hand the card to the user.
          </p>
          <button className="btn-primary" onClick={onClose}>
            Done
          </button>
        </div>
      }
    >
      <div className="space-y-4">
        <div
          className="w-full overflow-hidden rounded-lg [&>svg]:h-auto [&>svg]:w-full"
          dangerouslySetInnerHTML={{ __html: svg }}
        />
        <div className="flex flex-wrap gap-2">
          <button className="btn-ghost" onClick={downloadPng}>
            <Download className="h-4 w-4" /> Download card
          </button>
          <button className="btn-ghost" onClick={print}>
            <Printer className="h-4 w-4" /> Print
          </button>
          <button className="btn-ghost" onClick={copy}>
            {copied ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
            {copied ? "Copied" : "Copy password"}
          </button>
        </div>
      </div>
    </Modal>
  );
}
