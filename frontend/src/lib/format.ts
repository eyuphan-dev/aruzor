// Consistent 24-hour clock across the app, regardless of browser/OS locale defaults.
export function formatDateTime(value: string | number | Date): string {
  return new Date(value).toLocaleString("tr-TR", { hour12: false });
}

// Compact metric value for axes, tooltips and panel labels: large numbers
// collapse to K/M/B, small ones keep enough decimals to stay readable.
export function formatMetricValue(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  const abs = Math.abs(v);
  if (abs >= 1e9) return `${(v / 1e9).toFixed(2)}B`;
  if (abs >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
  if (abs >= 1e3) return `${(v / 1e3).toFixed(2)}K`;
  if (Number.isInteger(v)) return String(v);
  return v.toFixed(abs < 10 ? 2 : 1);
}
