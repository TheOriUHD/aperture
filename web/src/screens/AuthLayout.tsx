import type { ReactNode } from "react";
import { Aperture } from "lucide-react";

export function AuthLayout({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  return (
    <div className="h-full grid place-items-center bg-ink-900 px-4">
      <div className="w-full max-w-[380px]">
        <div className="flex flex-col items-center mb-7">
          <div className="h-12 w-12 rounded-xl bg-brand grid place-items-center mb-4">
            <Aperture className="h-7 w-7 text-white" />
          </div>
          <h1 className="text-xl font-medium text-fg">{title}</h1>
          <p className="text-sm text-fg-muted mt-1">{subtitle}</p>
        </div>
        <div className="panel p-6">{children}</div>
        <p className="text-center text-xs text-fg-faint mt-6">Aperture NVR</p>
      </div>
    </div>
  );
}
