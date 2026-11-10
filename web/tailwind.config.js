/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Neutral graphite surfaces, darkest -> raised. Measured from the reference
        // NVR screens the app is styled after: one flat page tone, panels a step up,
        // and a single "raised" grey for headers, active items and chips.
        ink: {
          950: "#0c0d0e", // behind video (letterbox)
          900: "#111314", // page, nav rail
          850: "#16181a", // subtle fill on the page (hover, sunken)
          800: "#1b1e21", // filled panel / card / input
          750: "#212427", // hover on a filled panel
          700: "#282a2e", // raised: app header, active item, chip
          650: "#303338", // hover on raised
          600: "#3a3e43",
        },
        line: {
          DEFAULT: "#222527", // hairlines: dividers, card outlines
          strong: "#2f3236", // control borders
        },
        fg: {
          DEFAULT: "#f2f4f5", // primary text
          muted: "#a5a8ac", // secondary (labels, table body)
          faint: "#6f7378", // tertiary (hints, axis labels)
        },
        brand: {
          DEFAULT: "#4797ff", // accent: links, active icons, selection marks
          solid: "#006fff", // filled primary buttons
          hover: "#2b86ff",
          deep: "#1b6fb0", // timeline continuous
          ink: "#022650", // selected row / active nav fill
        },
        rec: "#f0494e",
        danger: "#f2565b",
        warn: "#eb9608",
        motion: "#5fa8ff", // lighter blue: motion on the (dark-blue) recording rail
        // Playback timeline rail — one flat 3-state scheme, no patterns:
        // grey = nothing recorded, blue = recorded, lighter blue = motion/event.
        rail: {
          idle: "#34383d", // grey track (no recording)
          rec: "#05254D", // recorded footage (deep navy)
          event: "#0B4EAD", // motion/event duration (brighter blue)
        },
        online: "#39c66d",
      },
      fontFamily: {
        sans: ["Inter Variable", "Inter", "system-ui", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
      borderRadius: {
        md: "6px",
        lg: "8px",
      },
      boxShadow: {
        panel: "0 1px 2px rgba(0,0,0,0.4)",
        pop: "0 12px 32px rgba(0,0,0,0.55), 0 0 0 1px rgba(255,255,255,0.04)",
        drawer: "-16px 0 40px rgba(0,0,0,0.45)",
      },
      keyframes: {
        "rec-pulse": {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.45" },
        },
        "fade-in": {
          from: { opacity: "0" },
          to: { opacity: "1" },
        },
        "slide-in-right": {
          from: { transform: "translateX(24px)", opacity: "0" },
          to: { transform: "translateX(0)", opacity: "1" },
        },
        "pop-in": {
          from: { transform: "scale(0.98)", opacity: "0" },
          to: { transform: "scale(1)", opacity: "1" },
        },
      },
      animation: {
        "rec-pulse": "rec-pulse 1.6s ease-in-out infinite",
        "fade-in": "fade-in 150ms ease-out",
        "slide-in-right": "slide-in-right 180ms cubic-bezier(0.2, 0.8, 0.2, 1)",
        "pop-in": "pop-in 140ms ease-out",
      },
    },
  },
  plugins: [],
};
