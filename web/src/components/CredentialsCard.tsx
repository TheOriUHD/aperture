import { useState } from "react";
import { Download, Printer, Copy, Check } from "lucide-react";
import { Modal } from "./ui/Modal";

// cardSvg renders the credentials card as a self-contained SVG string, used for
// both on-screen preview and PNG/print export (one source of truth).
function cardSvg(loginUrl: string, email: string, password: string): string {
  const esc = (s: string) =>
    s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return `<svg xmlns="http://www.w3.org/2000/svg" width="760" height="460" viewBox="0 0 760 460" font-family="Inter, system-ui, sans-serif">
  <rect x="1" y="1" width="758" height="458" rx="18" fill="#14171c" stroke="#262b33" stroke-width="1.5"/>
  <rect x="1" y="1" width="758" height="6" rx="3" fill="#1b9df0"/>
  <g transform="translate(48,56)">
    <circle cx="13" cy="13" r="13" fill="#1b9df0"/>
    <circle cx="13" cy="13" r="5" fill="#14171c"/>
    <text x="40" y="11" fill="#eef0f2" font-size="17" font-weight="600" letter-spacing="2">APERTURE NVR</text>
    <text x="40" y="30" fill="#6c727a" font-size="12.5" letter-spacing="0.5">Account access card</text>
  </g>
  <line x1="48" y1="112" x2="712" y2="112" stroke="#20242b" stroke-width="1"/>
  <g transform="translate(48,150)">
    <text x="0" y="0" fill="#6c727a" font-size="12" letter-spacing="1.5">SIGN-IN ADDRESS</text>
    <text x="0" y="26" fill="#eef0f2" font-size="19">${esc(loginUrl)}</text>
  </g>
  <g transform="translate(48,222)">
    <text x="0" y="0" fill="#6c727a" font-size="12" letter-spacing="1.5">EMAIL</text>
    <text x="0" y="26" fill="#eef0f2" font-size="19">${esc(email)}</text>
  </g>
  <g transform="translate(48,290)">
    <text x="0" y="0" fill="#6c727a" font-size="12" letter-spacing="1.5">TEMPORARY PASSWORD</text>
    <rect x="0" y="14" width="664" height="52" rx="10" fill="#1e2228" stroke="#262b33"/>
    <text x="22" y="48" fill="#1b9df0" font-size="26" font-weight="600" font-family="ui-monospace, SFMono-Regular, Menlo, monospace" letter-spacing="2">${esc(password)}</text>
  </g>
  <text x="48" y="416" fill="#6c727a" font-size="12.5">You'll be asked to choose your own password the first time you sign in.</text>
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
      `<html><head><title>Aperture credentials</title></head><body style="margin:0;display:grid;place-items:center;height:100vh;background:#0d0f13">${svg}</body></html>`,
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
      footer={
        <div className="flex items-center justify-between w-full">
          <p className="text-xs text-amber max-w-[18rem]">
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
          className="w-full overflow-hidden rounded-lg [&>svg]:w-full [&>svg]:h-auto"
          dangerouslySetInnerHTML={{ __html: svg }}
        />
        <div className="flex flex-wrap gap-2">
          <button className="btn-secondary" onClick={downloadPng}>
            <Download className="h-4 w-4" /> Download card
          </button>
          <button className="btn-secondary" onClick={print}>
            <Printer className="h-4 w-4" /> Print
          </button>
          <button className="btn-secondary" onClick={copy}>
            {copied ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
            {copied ? "Copied" : "Copy password"}
          </button>
        </div>
      </div>
    </Modal>
  );
}
