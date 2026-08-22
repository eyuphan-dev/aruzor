// Palette for multi-series panels. Starts from the turquoise brand colors
// and moves through hues that stay distinguishable side by side in both
// light and dark themes.
const palette = [
  "#02c39a",
  "#00a896",
  "#f59e0b",
  "#3b82f6",
  "#ef4444",
  "#8b5cf6",
  "#10b981",
  "#ec4899",
  "#14b8a6",
  "#f97316",
  "#6366f1",
  "#84cc16",
];

export function seriesColor(index: number): string {
  return palette[index % palette.length];
}
