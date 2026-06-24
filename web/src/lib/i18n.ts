import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { setLocale } from "./format";

// German catalog for the high-visibility surfaces. Every t() call carries an
// English default, so untranslated keys fall back to English automatically and
// adding a language is just a catalog drop.
const de = {
  nav: {
    live: "Live",
    cameras: "Kameras",
    playback: "Wiedergabe",
    alarms: "Alarme",
    settings: "Einstellungen",
  },
  common: {
    save: "Speichern",
    cancel: "Abbrechen",
    delete: "Löschen",
    add: "Hinzufügen",
    close: "Schließen",
    edit: "Bearbeiten",
    saving: "Speichern…",
    enabled: "Aktiviert",
  },
  auth: {
    welcome: "Willkommen zurück",
    sub: "Bei Ihrem NVR anmelden",
    email: "E-Mail",
    password: "Passwort",
    signIn: "Anmelden",
    signingIn: "Anmelden…",
    invalid: "Ungültige E-Mail oder Passwort.",
    or: "oder",
    signInWith: "Anmelden mit {{name}}",
    ssoFailed: "Einmalanmeldung fehlgeschlagen. Versuchen Sie es erneut oder nutzen Sie Ihr Passwort.",
  },
  settings: {
    title: "Einstellungen",
    account: "Konto",
    name: "Name",
    email: "E-Mail",
    role: "Rolle",
    about: "Über",
    application: "Anwendung",
    version: "Version",
  },
  prefs: {
    title: "Persönliche Einstellungen",
    sub: "Diese gelten für Ihr Konto auf allen Geräten.",
    clock24: "24-Stunden-Uhr",
    clock24sub: "Zeiten als 14:30 statt 2:30 PM anzeigen",
    openTo: "Öffnen mit",
    liveWall: "Live-Wand",
    playback: "Wiedergabe",
    alarms: "Alarme",
    cameras: "Kameras",
    span: "Standard-Zeitspanne",
    density: "Dichte",
    comfortable: "Komfortabel",
    compact: "Kompakt",
    language: "Sprache",
    auto: "Automatisch",
    english: "Englisch",
    german: "Deutsch",
  },
};

const stored = (() => {
  try {
    return localStorage.getItem("aperture.lang") || "auto";
  } catch {
    return "auto";
  }
})();

function resolve(lang: string): "en" | "de" {
  if (lang === "de" || lang === "en") return lang;
  return navigator.language.toLowerCase().startsWith("de") ? "de" : "en";
}

void i18n.use(initReactI18next).init({
  resources: { de: { translation: de } },
  lng: resolve(stored),
  fallbackLng: false,
  interpolation: { escapeValue: false },
  returnEmptyString: false,
});
setLocale(resolve(stored));

// setLanguage accepts "en" | "de" | "auto" and applies it to both the string
// catalog and the date/number formatter.
export function setLanguage(lang: string): void {
  const r = resolve(lang);
  try {
    localStorage.setItem("aperture.lang", lang);
  } catch {
    /* ignore */
  }
  void i18n.changeLanguage(r);
  setLocale(r);
}

export default i18n;
