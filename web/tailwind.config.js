/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Charcoal surfaces (darkest -> elevated).
        ink: {
          900: "#0d0f13", // page
          850: "#101319",
          800: "#14171c", // panel
          750: "#181b21", // tile
          700: "#1e2228", // elevated
          650: "#242a31",
        },
        line: {
          DEFAULT: "#20242b", // dividers
          strong: "#262b33", // control borders
        },
        fg: {
          DEFAULT: "#eef0f2", // primary text
          muted: "#8b9199", // secondary
          faint: "#6c727a", // muted
        },
        brand: {
          DEFAULT: "#1b9df0",
          deep: "#1b6fb0", // timeline continuous
          ink: "#0b3a5c",
        },
        rec: "#f0494e",
        motion: "#f5a623",
        online: "#3fce7c",
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
        pop: "0 8px 24px rgba(0,0,0,0.45)",
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
      },
      animation: {
        "rec-pulse": "rec-pulse 1.6s ease-in-out infinite",
        "fade-in": "fade-in 150ms ease-out",
      },
    },
  },
  plugins: [],
};
