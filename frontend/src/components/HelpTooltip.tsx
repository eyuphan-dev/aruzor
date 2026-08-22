export function HelpTooltip({ text }: { text: string }) {
  return (
    <span
      title={text}
      className="inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-[var(--color-text-muted)] text-[10px] leading-none text-[var(--color-text-muted)]"
    >
      ?
    </span>
  );
}
