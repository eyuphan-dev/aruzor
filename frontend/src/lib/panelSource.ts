import type { Locale } from "./i18n/dictionaries";

// What a panel is actually looking at, derived from its query.
//
// A dashboard fills up with panels called "İşlemci Kullanımı", "Bellek
// Kullanımı", "Aktif Bağlantılar" — names that say what is being measured
// but never what is being measured *on*. Once a second exporter is added
// half the titles read as though they could belong to either, and two
// panels added from different places can end up with the identical name.
//
// The source is read off the PromQL rather than stored on the panel. That
// is deliberate: a stored label is a second copy of the truth that drifts
// the moment someone edits the query, whereas the metric namespace is the
// thing that actually determines where the numbers come from. It also means
// every dashboard saved before this existed gets labelled correctly with no
// migration.

export type PanelSource = {
  id: string;
  label: { tr: string; en: string };
  color: string;
};

// Matched in order, most specific first. The pattern looks for the metric
// namespace as a whole token so a label value that happens to contain
// "node_" somewhere cannot claim a panel.
const rules: { pattern: RegExp; source: PanelSource }[] = [
  {
    pattern: /\bkube_|\bkubelet_/,
    source: { id: "kubernetes", label: { tr: "Kubernetes", en: "Kubernetes" }, color: "#6366f1" },
  },
  {
    pattern: /\bcontainer_/,
    source: { id: "container", label: { tr: "Konteyner", en: "Containers" }, color: "#8b5cf6" },
  },
  {
    pattern: /\bnode_/,
    source: { id: "node", label: { tr: "Sunucu", en: "Server" }, color: "#02c39a" },
  },
  {
    pattern: /\bwindows_/,
    source: { id: "windows", label: { tr: "Windows", en: "Windows" }, color: "#3b82f6" },
  },
  {
    pattern: /\bpg_|\bpostgres_/,
    source: { id: "postgres", label: { tr: "PostgreSQL", en: "PostgreSQL" }, color: "#0ea5e9" },
  },
  {
    pattern: /\bmysql_/,
    source: { id: "mysql", label: { tr: "MySQL", en: "MySQL" }, color: "#f59e0b" },
  },
  {
    pattern: /\bredis_/,
    source: { id: "redis", label: { tr: "Redis", en: "Redis" }, color: "#ef4444" },
  },
  {
    pattern: /\bnginx_/,
    source: { id: "nginx", label: { tr: "Nginx", en: "Nginx" }, color: "#10b981" },
  },
  {
    pattern: /\bprobe_/,
    source: { id: "blackbox", label: { tr: "Erişilebilirlik", en: "Availability" }, color: "#14b8a6" },
  },
  {
    // Prometheus' own metrics are checked last: "up" and "scrape_*" exist
    // for every target, so a query that also mentions a real exporter
    // should be filed under that exporter instead.
    pattern: /\bprometheus_|\bscrape_|^\s*up\b|\(\s*up\b/,
    source: { id: "prometheus", label: { tr: "Prometheus", en: "Prometheus" }, color: "#f97316" },
  },
];

export function panelSource(promQL: string): PanelSource | null {
  for (const rule of rules) {
    if (rule.pattern.test(promQL)) return rule.source;
  }
  return null;
}

export function panelSourceLabel(promQL: string, locale: Locale): string | null {
  return panelSource(promQL)?.label[locale] ?? null;
}

// The grouping labels a query breaks its result down by, e.g. "instance"
// for `sum by (instance) (...)`.
//
// This is what separates two panels that measure the same thing at
// different resolutions — one CPU line for the whole fleet versus one line
// per server. Those two legitimately share a title, and without naming the
// difference the only way to tell them apart is to remember which is which.
export function panelGrouping(promQL: string): string[] {
  const groups: string[] = [];
  const re = /\bby\s*\(\s*([^)]*)\)/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(promQL)) !== null) {
    for (const raw of match[1].split(",")) {
      const label = raw.trim();
      if (label && !groups.includes(label)) groups.push(label);
    }
  }
  return groups;
}

// A short parenthetical that tells two same-named panels apart. Returns
// null when there is nothing meaningful to say, in which case the caller
// falls back to a counter — a bare number is a poor label, so it is only
// used when the query itself offers nothing better.
export function panelQualifier(promQL: string, locale: Locale): string | null {
  const groups = panelGrouping(promQL);
  if (groups.length === 0) return null;
  const joined = groups.join(", ");
  return locale === "tr" ? `${joined} bazında` : `by ${joined}`;
}

// Gives a panel a name that is not already taken on the dashboard.
//
// Adding the Node Exporter template and then accepting the discovered
// "Server" integration used to produce two panels both called "İşlemci
// Kullanımı" whose queries differed only in whether they aggregate per
// server — indistinguishable on screen. The qualifier names that difference
// where the query provides one.
export function uniquePanelTitle(title: string, promQL: string, taken: Set<string>, locale: Locale): string {
  if (!taken.has(title)) return title;

  const qualifier = panelQualifier(promQL, locale);
  if (qualifier) {
    const qualified = `${title} (${qualifier})`;
    if (!taken.has(qualified)) return qualified;
  }
  for (let n = 2; ; n++) {
    const numbered = `${title} (${n})`;
    if (!taken.has(numbered)) return numbered;
  }
}

// Names the panels on an already-saved dashboard so no two read the same.
//
// uniquePanelTitle above only runs when a panel is added, which does
// nothing for the dashboards that already collected a second "İşlemci
// Kullanımı" before it existed. Rather than rewriting saved titles — the
// operator may have named a panel deliberately, and a migration that edits
// their data to fix a display problem is the wrong trade — the distinction
// is computed at render time from the queries themselves.
//
// Panels whose title is already unique are left exactly as written.
export function disambiguateTitles(
  panels: { id: string; title: string; promql: string }[],
  locale: Locale,
): Record<string, string> {
  const counts = new Map<string, number>();
  for (const p of panels) counts.set(p.title, (counts.get(p.title) ?? 0) + 1);

  // Unique titles are reserved first so a qualified duplicate can never
  // collide with a panel that was already unambiguous.
  const used = new Set<string>();
  for (const p of panels) if (counts.get(p.title) === 1) used.add(p.title);

  const out: Record<string, string> = {};
  for (const p of panels) {
    if (counts.get(p.title) === 1) {
      out[p.id] = p.title;
      continue;
    }
    const qualifier = panelQualifier(p.promql, locale);
    const base = qualifier ? `${p.title} (${qualifier})` : p.title;
    let name = base;
    for (let n = 2; used.has(name); n++) name = `${base} (${n})`;
    used.add(name);
    out[p.id] = name;
  }
  return out;
}

// The panels that draw a chart another panel already draws.
//
// Adding a template whose panels partly overlap the dashboard used to copy
// the overlapping ones in again, leaving two identical charts under two
// identical names. Renaming one to "(2)" makes them addressable but still
// leaves the operator wondering which is which — the honest answer is that
// there is nothing to choose between them and one can go.
//
// The visualisation type is part of the comparison: the same query shown as
// a line and as a single stat is two different panels on purpose.
export function duplicatePanelIds(panels: { id: string; promql: string; panelType?: string }[]): Set<string> {
  const seen = new Set<string>();
  const duplicates = new Set<string>();
  for (const p of panels) {
    const key = `${p.panelType ?? "line"}\u0000${p.promql}`;
    if (seen.has(key)) duplicates.add(p.id);
    else seen.add(key);
  }
  return duplicates;
}
