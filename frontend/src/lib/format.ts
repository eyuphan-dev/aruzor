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

// Byte counts read as binary units everywhere an operator meets them (df,
// nginx, every hosting panel), so KiB/MiB is what matches the numbers they
// would see elsewhere on the same box.
export function formatBytes(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let value = Math.abs(v);
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  const sign = v < 0 ? "-" : "";
  // A byte count is never fractional to a reader, but a *rate* in bytes per
  // second is — and it arrives here as a plain number, so rounding has to
  // happen at the bottom unit too, or 0.6556134259 B/s reaches the screen.
  const shown = unit === 0 ? Math.round(value) : value.toFixed(value < 10 ? 2 : 1);
  return `${sign}${shown} ${units[unit]}`;
}

export function formatBytesPerSecond(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  return `${formatBytes(v)}/s`;
}

// Request rates spend most of their time below 1/s on a small server, where
// two decimals is the difference between a number and a flat zero.
export function formatRate(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  if (v >= 100) return v.toFixed(0);
  if (v >= 1) return v.toFixed(1);
  // A small site averages well under one request per second, and rounding
  // real traffic to "0.00" reads as "nothing is being served". Saying it is
  // below the smallest figure this format can show is both true and clearly
  // different from zero.
  if (v > 0 && v < 0.01) return "<0.01";
  return v.toFixed(2);
}

// Large counts in a table cell, where the exact figure matters less than
// its magnitude but the thousands separator still helps.
export function formatCount(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  return v.toLocaleString("tr-TR");
}
