import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api } from "./api";
import { useAuth } from "./auth";
import { setClock24 } from "./format";
import { setLanguage } from "./i18n";
import { DEFAULT_PREFS, type Preferences } from "./types";

interface PrefsState {
  prefs: Preferences;
  setPref: (partial: Partial<Preferences>) => void;
}

const Ctx = createContext<PrefsState | null>(null);

// PrefsProvider holds per-user preferences. They arrive on the user payload
// (/api/auth/me) and write through to /api/users/me/preferences on change.
// Precedence: per-user value → built-in default.
export function PrefsProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const [prefs, setPrefs] = useState<Preferences>(DEFAULT_PREFS);

  useEffect(() => {
    setPrefs({ ...DEFAULT_PREFS, ...(user?.preferences ?? {}) });
  }, [user]);

  useEffect(() => {
    setClock24(prefs.clock24);
  }, [prefs.clock24]);

  useEffect(() => {
    setLanguage(prefs.language);
  }, [prefs.language]);

  function setPref(partial: Partial<Preferences>) {
    setPrefs((cur) => {
      const next = { ...cur, ...partial };
      void api.put("/api/users/me/preferences", next).catch(() => {});
      return next;
    });
  }

  return <Ctx.Provider value={{ prefs, setPref }}>{children}</Ctx.Provider>;
}

export function usePrefs(): PrefsState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("usePrefs must be used within PrefsProvider");
  return ctx;
}
