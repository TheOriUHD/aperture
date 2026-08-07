// ExtIcon — the one consistent icon treatment for an extension across the manager
// row and the marketplace grid: the extension's own PNG (served as an <img> from
// its CSP'd asset route — never inlined markup), or a neutral monogram fallback so
// an extension with no icon still reads as a distinct, finished item rather than a
// generic glyph.
export function ExtIcon({ icon, name, size = 40 }: { icon?: string; name: string; size?: number }) {
  const letter = (name.trim()[0] || "?").toUpperCase();
  return (
    <div
      className="grid shrink-0 place-items-center overflow-hidden rounded-lg border border-line bg-ink-700"
      style={{ width: size, height: size }}
    >
      {icon ? (
        <img src={icon} alt="" className="h-full w-full object-cover" />
      ) : (
        <span className="font-semibold leading-none text-fg-muted" style={{ fontSize: Math.round(size * 0.42) }}>
          {letter}
        </span>
      )}
    </div>
  );
}
