import clsx from "clsx";

export function Switch({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={clsx(
        "relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors duration-150",
        checked ? "bg-brand" : "bg-ink-650",
      )}
    >
      <span
        className={clsx(
          "inline-block rounded-full bg-white shadow transition-transform duration-150",
          checked ? "translate-x-[22px]" : "translate-x-[3px]",
        )}
        style={{ height: 18, width: 18 }}
      />
    </button>
  );
}
