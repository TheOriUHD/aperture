import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { useLocation } from "react-router-dom";

// Tracks unread motion alarms and reflects the count in the tab title + a
// favicon badge. Fed by the live SSE stream (DeviceNotifications), reset when
// the user opens the Alarms screen. Persisted per-device and synced across tabs.
const COUNT_KEY = "aperture.unreadAlarms";

interface Ctx {
  count: number;
  bump: () => void;
  reset: () => void;
}
const UnreadCtx = createContext<Ctx>({ count: 0, bump: () => {}, reset: () => {} });

export function useUnreadAlarms(): Ctx {
  return useContext(UnreadCtx);
}

let baseIcon: HTMLImageElement | null = null;

function badgeFavicon(count: number): string {
  if (count <= 0 || !baseIcon || !baseIcon.complete) return "/favicon-32.png";
  const canvas = document.createElement("canvas");
  canvas.width = 32;
  canvas.height = 32;
  const ctx = canvas.getContext("2d");
  if (!ctx) return "/favicon-32.png";
  ctx.drawImage(baseIcon, 0, 0, 32, 32);
  ctx.beginPath();
  ctx.arc(23, 9, 9, 0, Math.PI * 2);
  ctx.fillStyle = "#e0484f";
  ctx.fill();
  ctx.fillStyle = "#fff";
  ctx.font = "bold 13px sans-serif";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillText(count > 9 ? "9+" : String(count), 23, 10);
  try {
    return canvas.toDataURL("image/png");
  } catch {
    return "/favicon-32.png";
  }
}

export function UnreadAlarmsProvider({ children }: { children: ReactNode }) {
  const [count, setCount] = useState<number>(() => Number(localStorage.getItem(COUNT_KEY) || 0));
  const location = useLocation();

  useEffect(() => {
    const img = new Image();
    img.onload = () => {
      baseIcon = img;
    };
    img.src = "/favicon-32.png";
  }, []);

  const persist = useCallback((n: number) => {
    setCount(n);
    localStorage.setItem(COUNT_KEY, String(n));
  }, []);

  const bump = useCallback(
    () => persist(Number(localStorage.getItem(COUNT_KEY) || 0) + 1),
    [persist],
  );
  const reset = useCallback(() => persist(0), [persist]);

  // Sync across tabs.
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === COUNT_KEY) setCount(Number(e.newValue || 0));
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  // Clear when the user looks at the Alarms screen.
  useEffect(() => {
    if (location.pathname === "/alarms") reset();
  }, [location.pathname, reset]);

  // Reflect in the tab title + favicon.
  useEffect(() => {
    document.title = count > 0 ? `(${count > 9 ? "9+" : count}) Aperture` : "Aperture";
    requestAnimationFrame(() => {
      const link = document.getElementById("app-favicon") as HTMLLinkElement | null;
      if (link) link.href = badgeFavicon(count);
    });
  }, [count]);

  return <UnreadCtx.Provider value={{ count, bump, reset }}>{children}</UnreadCtx.Provider>;
}
