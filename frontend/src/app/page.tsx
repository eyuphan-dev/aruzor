"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Responsive, WidthProvider, type Layout } from "react-grid-layout";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import { useI18n } from "@/lib/i18n/context";
import { defaultQueries } from "@/lib/defaultQueries";
import { DashboardPanel } from "@/components/DashboardPanel";
import { TimeRangeControls } from "@/components/TimeRangeControls";
import { ActionMenu, type Action } from "@/components/ActionMenu";
import { ShortcutsHelp } from "@/components/ShortcutsHelp";
import { DiscoveryPanel } from "@/components/DiscoveryPanel";
import { TrafficStrip } from "@/components/TrafficStrip";
import { ShareDialog } from "@/components/ShareDialog";
import {
  getDashboard,
  saveDashboard,
  listDashboards,
  deleteDashboard,
  getMetricNames,
  getLabelValues,
  type PanelLayout,
  type PanelType,
  type DashboardVariable,
  type DashboardAnnotation,
  type DashboardDefinition,
  type DashboardSummary,
} from "@/lib/api";
import type { DiscoveredIntegration } from "@/lib/api";
import { dashboardTemplates } from "@/lib/templates";
import { useAuth } from "@/lib/auth/context";
import { hasMinRole } from "@/lib/auth/roles";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { genId } from "@/lib/id";
import { describeMetric } from "@/lib/metricDescriptions";
import { disambiguateTitles, duplicatePanelIds, uniquePanelTitle } from "@/lib/panelSource";

const ReactGridLayout = WidthProvider(Responsive);

// A 12-column grid on a 390px phone gives every panel roughly 150px of
// width: titles truncate to two characters and the charts become unreadable.
// Below the "sm" breakpoint the grid collapses to a single column and each
// panel gets a workable minimum height instead.
const GRID_BREAKPOINTS = { lg: 1200, md: 996, sm: 768, xs: 480, xxs: 0 };
const GRID_COLS = { lg: 12, md: 12, sm: 1, xs: 1, xxs: 1 };
const STACKED_BREAKPOINTS = new Set(["sm", "xs", "xxs"]);
const STACKED_MIN_HEIGHT = 4;
// Half a row: what every built-in template and every discovered panel uses,
// and the width a panel is restored to when its stored one is unusable.
const DEFAULT_PANEL_WIDTH = 6;
const ACTIVE_DASHBOARD_STORAGE_KEY = "aruzor-active-dashboard";
const HOST_STORAGE_KEY = "aruzor.dashboard.host";
const RANGE_STORAGE_KEY = "aruzor-time-range";
const REFRESH_STORAGE_KEY = "aruzor-refresh-ms";

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Substitutes $name placeholders with their selected values. Longest names
// are matched first and a word-boundary check (no identifier char right
// after the name) prevents "$instance" from clobbering "$instance_id".
function resolvePromQL(promql: string, values: Record<string, string>): string {
  const names = Object.keys(values).sort((a, b) => b.length - a.length);
  if (names.length === 0) return promql;
  const pattern = new RegExp(`\\$(${names.map(escapeRegExp).join("|")})(?![a-zA-Z0-9_])`, "g");
  return promql.replace(pattern, (_match, name: string) => values[name]);
}

export default function DashboardPage() {
  const { t, locale } = useI18n();
  const { session } = useAuth();
  const canEdit = hasMinRole(session?.role, "editor");
  const [dashboards, setDashboards] = useState<DashboardSummary[]>([]);
  const [dashboardId, setDashboardId] = useState<string | null>(null);
  const [dashboardTitle, setDashboardTitle] = useState("");
  const [creatingDashboard, setCreatingDashboard] = useState(false);
  // Dashboards created in this session and not yet saved to the server.
  // Selecting one triggers the load effect below, which would fetch an id
  // the server has never heard of and fall back to the default definition —
  // silently turning "new dashboard" into "copy of the default one".
  const unsavedDashboards = useRef<Set<string>>(new Set());
  const [newDashboardName, setNewDashboardName] = useState("");
  const [confirmingDeleteDashboard, setConfirmingDeleteDashboard] = useState(false);
  const [def, setDef] = useState<DashboardDefinition | null>(null);
  const [varValues, setVarValues] = useState<Record<string, string>>({});
  const [varOptions, setVarOptions] = useState<Record<string, string[]>>({});
  const [editMode, setEditMode] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);

  // Host filter. Empty means every server. Kept in the browser so the choice
  // survives a reload the way the time range does.
  const [host, setHost] = useState("");
  const [hosts, setHosts] = useState<string[]>([]);

  // Which grid breakpoint react-grid-layout is currently rendering at.
  const [breakpoint, setBreakpoint] = useState<string>("lg");
  const stacked = STACKED_BREAKPOINTS.has(breakpoint);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [metrics, setMetrics] = useState<string[]>([]);
  const [newPanelTitle, setNewPanelTitle] = useState("");
  const [newPanelMetric, setNewPanelMetric] = useState("");
  const [newPanelType, setNewPanelType] = useState<PanelType>("line");
  const [newPanelThreshold, setNewPanelThreshold] = useState("");
  const [selectedTemplateId, setSelectedTemplateId] = useState("");
  const [newVarName, setNewVarName] = useState("");
  const [newVarLabel, setNewVarLabel] = useState("");
  const [newAnnotationLabel, setNewAnnotationLabel] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Time window + refresh cadence are dashboard-wide so every panel shows
  // the same period; both persist per browser so a reload doesn't throw
  // the user back to a default they didn't pick.
  const [rangeSeconds, setRangeSeconds] = useState(3600);
  const [refreshMs, setRefreshMs] = useState(10_000);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);

  useEffect(() => {
    const savedRange = Number(window.localStorage.getItem(RANGE_STORAGE_KEY));
    const savedRefresh = Number(window.localStorage.getItem(REFRESH_STORAGE_KEY));
    if (Number.isFinite(savedRange) && savedRange > 0) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setRangeSeconds(savedRange);
    }
    if (Number.isFinite(savedRefresh) && savedRefresh >= 0 && window.localStorage.getItem(REFRESH_STORAGE_KEY)) {
      setRefreshMs(savedRefresh);
    }
  }, []);

  const changeRange = (seconds: number) => {
    setRangeSeconds(seconds);
    window.localStorage.setItem(RANGE_STORAGE_KEY, String(seconds));
  };

  const changeRefreshMs = (ms: number) => {
    setRefreshMs(ms);
    window.localStorage.setItem(REFRESH_STORAGE_KEY, String(ms));
  };

  const refreshNow = () => setRefreshNonce((n) => n + 1);

  // Single-key shortcuts, deliberately ignored while the user is typing so
  // they can't fire from inside a query/title field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      const tag = el?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el?.isContentEditable) return;

      if (e.key === "r") {
        setRefreshNonce((n) => n + 1);
      } else if (e.key === "e" && canEdit) {
        setEditMode((v) => !v);
      } else if (e.key === "?") {
        setShortcutsOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [canEdit]);

  const defaultDef = useMemo<DashboardDefinition>(
    () => ({
      panels: [
        { id: "cpu", title: t.dashboard.cpu, promql: defaultQueries.cpu, color: "#02c39a", x: 0, y: 0, w: 6, h: 4 },
        { id: "memory", title: t.dashboard.memory, promql: defaultQueries.memory, color: "#00a896", x: 6, y: 0, w: 6, h: 4 },
        { id: "disk", title: t.dashboard.disk, promql: defaultQueries.disk, color: "#f59e0b", x: 0, y: 4, w: 6, h: 4 },
        { id: "network", title: t.dashboard.network, promql: defaultQueries.network, color: "#10b981", x: 6, y: 4, w: 6, h: 4 },
      ],
      variables: [],
      annotations: [],
    }),
    [t],
  );

  // On first load: fetch the dashboard list and pick one to show — the
  // last one this browser had open (if it still exists), otherwise the
  // first in the list, otherwise fall back to a fresh unsaved "default"
  // dashboard (nothing persisted until the user hits Save, same as before
  // multi-dashboard support existed).
  useEffect(() => {
    listDashboards()
      .then((list) => {
        setDashboards(list);
        const remembered = window.localStorage.getItem(ACTIVE_DASHBOARD_STORAGE_KEY);
        const initial = list.find((d) => d.id === remembered)?.id ?? list[0]?.id ?? "default";
        setDashboardId(initial);
      })
      .catch((err: Error) => {
        setError(err.message);
        setDashboardId("default");
      });
  }, []);

  useEffect(() => {
    const stored = window.localStorage.getItem(HOST_STORAGE_KEY);
    // The preference only exists in localStorage, so the "all servers"
    // default rendered on the server is corrected once after mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (stored) setHost(stored);
    // The list is only worth showing when there is more than one server to
    // choose between; a single-host install gets no extra control.
    getLabelValues("instance")
      .then(setHosts)
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    if (!dashboardId) return;
    window.localStorage.setItem(ACTIVE_DASHBOARD_STORAGE_KEY, dashboardId);
    // A dashboard that only exists in the browser has nothing to fetch, and
    // fetching anyway would overwrite the empty definition just created.
    if (unsavedDashboards.current.has(dashboardId)) return;
    getDashboard(dashboardId)
      .then((saved) => {
        setDef(saved?.definition ?? defaultDef);
        setDashboardTitle(saved?.title ?? t.dashboard.untitled);
      })
      .catch((err: Error) => {
        setError(err.message);
        setDef(defaultDef);
        setDashboardTitle(t.dashboard.untitled);
      });
    // Switching to an existing dashboard should always drop out of edit
    // mode for the previous one. A freshly created dashboard returns above
    // and keeps the edit mode its creation turned on.
    setEditMode(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dashboardId]);

  useEffect(() => {
    if (editMode) {
      getMetricNames()
        .then(setMetrics)
        .catch(() => setMetrics([]));
    }
  }, [editMode]);

  // Fetch each variable's possible values and auto-select the first one.
  useEffect(() => {
    if (!def) return;
    def.variables.forEach((v) => {
      if (varOptions[v.name]) return;
      getLabelValues(v.label)
        .then((values) => {
          setVarOptions((prev) => ({ ...prev, [v.name]: values }));
          setVarValues((prev) => (prev[v.name] ? prev : { ...prev, [v.name]: values[0] ?? "" }));
        })
        .catch(() => setVarOptions((prev) => ({ ...prev, [v.name]: [] })));
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [def?.variables]);

  const handleLayoutChange = (layout: Layout[]) => {
    if (!def) return;
    // The single-column phone layout is generated, not authored. Writing it
    // back would overwrite the real desktop arrangement the moment someone
    // opened the dashboard on a phone.
    if (stacked) return;
    // A second, shape-based guard on the same thing. `breakpoint` is state
    // updated by a callback, so on the very first render at a phone width it
    // still says "lg" while the grid has already laid out and reported the
    // stacked layout — which is exactly how dashboards ended up saved with
    // every panel one column wide. A layout where nothing is wider than one
    // column is never something anyone arranged on a twelve-column grid.
    if (layout.length > 1 && layout.every((l) => l.w <= 1)) return;
    const byId = new Map(layout.map((l) => [l.i, l]));
    setDef({
      ...def,
      panels: def.panels.map((p) => {
        const l = byId.get(p.id);
        return l ? { ...p, x: l.x, y: l.y, w: l.w, h: l.h } : p;
      }),
    });
  };

  const removePanel = (id: string) => {
    setDef((prev) => (prev ? { ...prev, panels: prev.panels.filter((p) => p.id !== id) } : prev));
  };

  // Changing a panel's chart type is a one-click action from the panel
  // header, available outside edit mode too — flipping a view is a way of
  // reading the data, not of restructuring the dashboard. It only becomes
  // permanent once the dashboard is saved.
  const changePanelType = (id: string, panelType: PanelType) => {
    setDef((prev) =>
      prev ? { ...prev, panels: prev.panels.map((p) => (p.id === id ? { ...p, panelType } : p)) } : prev,
    );
  };

  const duplicatePanel = (id: string) => {
    setDef((prev) => {
      if (!prev) return prev;
      const source = prev.panels.find((p) => p.id === id);
      if (!source) return prev;
      const maxY = prev.panels.reduce((m, p) => Math.max(m, p.y + p.h), 0);
      return { ...prev, panels: [...prev.panels, { ...source, id: genId(), x: 0, y: maxY }] };
    });
  };

  const addPanel = () => {
    if (!newPanelMetric || !def) return;
    const maxY = def.panels.reduce((m, p) => Math.max(m, p.y + p.h), 0);
    const threshold = newPanelThreshold.trim() === "" ? undefined : Number(newPanelThreshold);
    const panel: PanelLayout = {
      id: genId(),
      title: newPanelTitle || newPanelMetric,
      promql: newPanelMetric,
      color: "#02c39a",
      x: 0,
      y: maxY,
      w: 6,
      h: 4,
      panelType: newPanelType,
      threshold: newPanelType === "line" && threshold !== undefined && !Number.isNaN(threshold) ? threshold : undefined,
    };
    setDef({ ...def, panels: [...def.panels, panel] });
    setNewPanelTitle("");
    setNewPanelMetric("");
    setNewPanelType("line");
    setNewPanelThreshold("");
  };

  const addAnnotation = () => {
    const label = newAnnotationLabel.trim();
    if (!label || !def) return;
    const annotation: DashboardAnnotation = { id: genId(), label, at: Math.floor(Date.now() / 1000) };
    setDef({ ...def, annotations: [...def.annotations, annotation] });
    setNewAnnotationLabel("");
  };

  const removeAnnotation = (id: string) => {
    setDef((prev) => (prev ? { ...prev, annotations: prev.annotations.filter((a) => a.id !== id) } : prev));
  };

  // A template is considered already added when every one of its panel
  // queries is already present on the dashboard — this also covers
  // dashboards loaded from a previous save, not just the current session.
  const addedTemplateIds = useMemo(() => {
    if (!def) return new Set<string>();
    const existingPromql = new Set(def.panels.map((p) => p.promql));
    return new Set(
      dashboardTemplates.filter((tpl) => tpl.panels.every((p) => existingPromql.has(p.promql))).map((tpl) => tpl.id),
    );
  }, [def]);

  const addTemplate = () => {
    const template = dashboardTemplates.find((tpl) => tpl.id === selectedTemplateId);
    if (!template || !def || addedTemplateIds.has(template.id)) {
      if (template && addedTemplateIds.has(template.id)) setNotice(t.dashboard.templateAlreadyAddedNotice);
      return;
    }

    let nextY = def.panels.reduce((m, p) => Math.max(m, p.y + p.h), 0);
    const added: PanelLayout[] = [];
    // Titles already in use, updated as panels are added so two panels
    // inside the same template cannot collide with each other either.
    const takenTitles = new Set(def.panels.map((p) => p.title));
    // A template is only offered when at least one of its panels is missing,
    // so the ones already present have to be skipped individually — adding
    // them again is what produced identical charts side by side.
    const existingPromql = new Set(def.panels.map((p) => p.promql));
    let skipped = 0;
    let x = 0;
    for (const p of template.panels) {
      if (existingPromql.has(p.promql)) {
        skipped++;
        continue;
      }
      if (x + p.w > 12) {
        x = 0;
        nextY += p.h;
      }
      existingPromql.add(p.promql);
      const title = uniquePanelTitle(p.title[locale], p.promql, takenTitles, locale);
      takenTitles.add(title);
      added.push({
        id: genId(),
        title,
        promql: p.promql,
        color: p.color,
        x,
        y: nextY,
        w: p.w,
        h: p.h,
      });
      x += p.w;
    }

    if (added.length > 0) setDef({ ...def, panels: [...def.panels, ...added] });
    setSelectedTemplateId("");
    setNotice(
      skipped > 0
        ? `${added.length} ${t.discovery.addedCount} · ${skipped} ${t.discovery.skippedCount}`
        : t.dashboard.templateAddedNotice,
    );
  };

  // Turns a detected exporter into real panels. Laid out two-per-row at the
  // desktop width, which is how the built-in templates are arranged too.
  const addDiscovered = (integration: DiscoveredIntegration) => {
    if (!def) return;
    let nextY = def.panels.reduce((m, p) => Math.max(m, p.y + p.h), 0);
    let x = 0;
    const added: PanelLayout[] = [];
    const existingPromql = new Set(def.panels.map((p) => p.promql));
    const takenTitles = new Set(def.panels.map((p) => p.title));
    let skipped = 0;

    for (const panel of integration.panels) {
      // A discovered integration overlaps the built-in templates by design —
      // both describe the same exporter. Adding a panel whose query is
      // already on the dashboard would draw the identical chart twice under
      // the identical name, which is what made these hard to tell apart.
      if (existingPromql.has(panel.promql)) {
        skipped++;
        continue;
      }
      if (x + 6 > 12) {
        x = 0;
        nextY += 4;
      }
      const title = uniquePanelTitle(
        locale === "tr" ? panel.titleTr : panel.titleEn,
        panel.promql,
        takenTitles,
        locale,
      );
      takenTitles.add(title);
      existingPromql.add(panel.promql);
      added.push({
        id: genId(),
        title,
        promql: panel.promql,
        color: "#02c39a",
        x,
        y: nextY,
        w: 6,
        h: 4,
        panelType: panel.unit === "percent" ? "area" : "line",
      });
      x += 6;
    }

    if (added.length > 0) setDef({ ...def, panels: [...def.panels, ...added] });
    // Saying nothing when everything was skipped would look like the button
    // did not work.
    setNotice(
      skipped > 0
        ? `${added.length} ${t.discovery.addedCount} · ${skipped} ${t.discovery.skippedCount}`
        : t.discovery.addedNotice,
    );
  };

  const addVariable = () => {
    if (!newVarName.trim() || !newVarLabel.trim() || !def) return;
    const variable: DashboardVariable = { name: newVarName.trim(), label: newVarLabel.trim() };
    setDef({ ...def, variables: [...def.variables.filter((v) => v.name !== variable.name), variable] });
    setNewVarName("");
    setNewVarLabel("");
  };

  const removeVariable = (name: string) => {
    setDef((prev) => (prev ? { ...prev, variables: prev.variables.filter((v) => v.name !== name) } : prev));
    setVarValues((prev) => {
      const next = { ...prev };
      delete next[name];
      return next;
    });
  };

  const handleSave = async () => {
    if (!def || !dashboardId) return;
    setSaving(true);
    setError(null);
    try {
      await saveDashboard(dashboardId, dashboardTitle || t.dashboard.untitled, def);
      // It exists on the server now, so it should be re-fetched like any
      // other dashboard from here on.
      unsavedDashboards.current.delete(dashboardId);
      setNotice(t.dashboard.saved);
      setEditMode(false);
      listDashboards().then(setDashboards).catch(() => undefined);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    if (!dashboardId) return;
    // Nothing saved to revert to yet — discarding here would replace the
    // empty dashboard with the default panels.
    if (unsavedDashboards.current.has(dashboardId)) {
      setDef({ panels: [], variables: [], annotations: [] });
      setEditMode(false);
      return;
    }
    getDashboard(dashboardId).then((saved) => {
      setDef(saved?.definition ?? defaultDef);
      setDashboardTitle(saved?.title ?? t.dashboard.untitled);
    });
    setEditMode(false);
  };

  const handleExport = () => {
    if (!def) return;
    const blob = new Blob([JSON.stringify({ title: dashboardTitle, definition: def }, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "aruzor-dashboard.json";
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleCreateDashboard = () => {
    const title = newDashboardName.trim();
    if (!title) return;
    const id = genId();
    unsavedDashboards.current.add(id);
    setDashboards((prev) => [{ id, title, updatedAt: new Date().toISOString() }, ...prev]);
    setDashboardId(id);
    setDef({ panels: [], variables: [], annotations: [] });
    setDashboardTitle(title);
    setEditMode(true);
    setNewDashboardName("");
    setCreatingDashboard(false);
  };

  const handleDeleteDashboard = async () => {
    if (!dashboardId) return;
    try {
      await deleteDashboard(dashboardId);
      const remaining = dashboards.filter((d) => d.id !== dashboardId);
      setDashboards(remaining);
      setDashboardId(remaining[0]?.id ?? "default");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setConfirmingDeleteDashboard(false);
    }
  };

  const handleImportClick = () => fileInputRef.current?.click();

  const handleImportFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;

    setError(null);
    file
      .text()
      .then((text) => {
        const parsed = JSON.parse(text);
        const rawDef = Array.isArray(parsed) ? parsed : parsed.definition;
        const rawPanels = Array.isArray(rawDef) ? rawDef : rawDef?.panels;
        const rawVariables = Array.isArray(rawDef) ? [] : (rawDef?.variables ?? []);
        const rawAnnotations = Array.isArray(rawDef) ? [] : (rawDef?.annotations ?? []);
        if (!Array.isArray(rawPanels)) throw new Error("invalid shape");

        const panels: PanelLayout[] = rawPanels.map((p, i) => ({
          id: typeof p.id === "string" ? p.id : genId(),
          title: typeof p.title === "string" ? p.title : `Panel ${i + 1}`,
          promql: String(p.promql ?? ""),
          color: typeof p.color === "string" ? p.color : "#02c39a",
          x: Number(p.x) || 0,
          y: Number(p.y) || 0,
          w: Number(p.w) || 6,
          h: Number(p.h) || 4,
          panelType: p.panelType === "stat" || p.panelType === "gauge" ? p.panelType : "line",
          threshold: typeof p.threshold === "number" ? p.threshold : undefined,
        }));
        if (panels.some((p) => !p.promql)) throw new Error("missing promql");

        const variables: DashboardVariable[] = Array.isArray(rawVariables)
          ? rawVariables
              .filter((v) => typeof v?.name === "string" && typeof v?.label === "string")
              .map((v) => ({ name: v.name, label: v.label }))
          : [];

        const annotations: DashboardAnnotation[] = Array.isArray(rawAnnotations)
          ? rawAnnotations
              .filter((a) => typeof a?.label === "string" && typeof a?.at === "number")
              .map((a) => ({ id: typeof a.id === "string" ? a.id : genId(), label: a.label, at: a.at }))
          : [];

        setDef({ panels, variables, annotations });
        setEditMode(true);
        setNotice(t.dashboard.importedNotice);
      })
      .catch(() => setError(t.dashboard.importError));
  };

  if (!def) {
    return <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>;
  }

  // Two panels reading "İşlemci Kullanımı" are indistinguishable on screen
  // even though their queries differ. The stored titles are left untouched;
  // only what is drawn is disambiguated.
  const shownTitles = disambiguateTitles(def.panels, locale);
  const duplicatePanels = duplicatePanelIds(def.panels);

  // Dashboards saved by an older build could have the phone layout written
  // over them, leaving panels one column of twelve wide — 8% of the screen,
  // too narrow to draw a chart in. That is not a width anyone chose, so it
  // is corrected on the way to the grid. The stored value is left alone
  // until the dashboard is next saved, so nothing is rewritten behind the
  // operator's back.
  const layout: Layout[] = def.panels.map((p) => ({
    i: p.id,
    x: p.x,
    y: p.y,
    w: p.w <= 1 ? DEFAULT_PANEL_WIDTH : p.w,
    h: p.h,
  }));

  // The stored layout is the desktop one. Phones get a generated single
  // column in the same visual order rather than a squeezed version of it.
  const stackedLayout: Layout[] = [...def.panels]
    .sort((a, b) => a.y - b.y || a.x - b.x)
    .map((p, i) => ({ i: p.id, x: 0, y: i, w: 1, h: Math.max(p.h, STACKED_MIN_HEIGHT) }));

  const layouts = {
    lg: layout,
    md: layout,
    sm: stackedLayout,
    xs: stackedLayout,
    xxs: stackedLayout,
  };

  // Everything that is not the dashboard picker, the time controls or the
  // edit toggle. Declared here so the wide layout and the phone layout show
  // the same set instead of drifting apart.
  const secondaryActions: Action[] = [
    { key: "export", label: t.dashboard.export, onClick: handleExport },
    ...(canEdit
      ? [
          { key: "import", label: t.dashboard.import, onClick: handleImportClick },
          {
            key: "share",
            label: t.dashboard.share,
            onClick: () => {
              // Sharing resolves a dashboard id on the server; one that only
              // exists in this browser would 404 and look broken.
              if (dashboardId && unsavedDashboards.current.has(dashboardId)) {
                setNotice(t.dashboard.shareUnsaved);
                return;
              }
              setShareOpen(true);
            },
          },
        ]
      : []),
    { key: "shortcuts", label: t.dashboard.shortcutsTitle, onClick: () => setShortcutsOpen(true) },
    ...(canEdit && dashboards.length > 1
      ? [{ key: "delete", label: t.dashboard.deleteDashboard, onClick: () => setConfirmingDeleteDashboard(true), danger: true }]
      : []),
  ];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={dashboardId ?? ""}
            onChange={(e) => setDashboardId(e.target.value)}
            className="aruzor-select aruzor-select-auto min-w-[160px] font-medium"
          >
            {dashboards.length === 0 && <option value="default">{t.dashboard.untitled}</option>}
            {dashboards.map((d) => (
              <option key={d.id} value={d.id}>
                {d.title}
              </option>
            ))}
          </select>
          {canEdit && !creatingDashboard && (
            <button
              onClick={() => setCreatingDashboard(true)}
              title={t.dashboard.newDashboard}
              className="flex min-h-9 min-w-9 items-center justify-center rounded-md border border-[var(--color-border)] px-2 text-sm hover:bg-[var(--color-border)]/30"
            >
              +
            </button>
          )}
          {canEdit && creatingDashboard && (
            <div className="flex items-center gap-1">
              <input
                autoFocus
                value={newDashboardName}
                onChange={(e) => setNewDashboardName(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleCreateDashboard()}
                placeholder={t.dashboard.newDashboardPlaceholder}
                className="aruzor-input w-40"
              />
              <button
                onClick={handleCreateDashboard}
                disabled={!newDashboardName.trim()}
                className="rounded-md bg-[var(--color-primary)] px-2 py-1.5 text-sm text-white disabled:opacity-60"
              >
                {t.dashboard.createDashboard}
              </button>
              <button
                onClick={() => {
                  setCreatingDashboard(false);
                  setNewDashboardName("");
                }}
                className="rounded-md border border-[var(--color-border)] px-2 py-1.5 text-sm"
              >
                ✕
              </button>
            </div>
          )}
        </div>
        <p
          className={`w-full text-sm text-[var(--color-text-muted)] sm:w-auto sm:flex-1 ${
            editMode ? "" : "hidden sm:block"
          }`}
        >
          {editMode ? t.dashboard.editHint : t.dashboard.subtitle}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <TimeRangeControls
            rangeSeconds={rangeSeconds}
            onRangeChange={changeRange}
            refreshMs={refreshMs}
            onRefreshMsChange={changeRefreshMs}
            onRefreshNow={refreshNow}
          />
          {hosts.length > 1 && (
            <select
              value={host}
              onChange={(e) => {
                setHost(e.target.value);
                window.localStorage.setItem(HOST_STORAGE_KEY, e.target.value);
              }}
              aria-label={t.dashboard.hostLabel}
              className="aruzor-select aruzor-select-auto"
            >
              <option value="">{t.dashboard.hostAll}</option>
              {hosts.map((h) => (
                <option key={h} value={h}>
                  {h}
                </option>
              ))}
            </select>
          )}
          <ActionMenu actions={secondaryActions} />
          {canEdit && (
            <>
              <input ref={fileInputRef} type="file" accept="application/json" onChange={handleImportFile} className="hidden" />
              {editMode ? (
                <>
                  <button
                    onClick={handleCancel}
                    className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm hover:bg-[var(--color-border)]/30"
                  >
                    {t.dashboard.cancel}
                  </button>
                  <button
                    onClick={handleSave}
                    disabled={saving}
                    className="rounded-md bg-[var(--color-primary)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
                  >
                    {saving ? t.dashboard.saving : t.dashboard.save}
                  </button>
                </>
              ) : (
                <button
                  onClick={() => setEditMode(true)}
                  className="rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm hover:bg-[var(--color-border)]/30"
                >
                  {t.dashboard.edit}
                </button>
              )}
            </>
          )}
        </div>
      </div>

      {notice && <p className="text-sm text-[var(--color-success)]">{notice}</p>}
      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      {def.variables.length > 0 && (
        <div className="flex flex-wrap items-end gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          {def.variables.map((v) => (
            <div key={v.name} className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">${v.name}</label>
              <select
                value={varValues[v.name] ?? ""}
                onChange={(e) => setVarValues((prev) => ({ ...prev, [v.name]: e.target.value }))}
                className="aruzor-select min-w-[160px]"
              >
                {(varOptions[v.name] ?? []).map((val) => (
                  <option key={val} value={val}>
                    {val}
                  </option>
                ))}
              </select>
            </div>
          ))}
        </div>
      )}

      {editMode && (
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.newDashboardPlaceholder}</label>
              <input
                value={dashboardTitle}
                onChange={(e) => setDashboardTitle(e.target.value)}
                className="aruzor-input min-w-[180px]"
              />
            </div>

            <div className="h-8 w-px bg-[var(--color-border)]" />

            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.templateLabel}</label>
              <select
                value={selectedTemplateId}
                onChange={(e) => setSelectedTemplateId(e.target.value)}
                className="aruzor-select min-w-[220px]"
              >
                <option value="">{t.dashboard.templatePlaceholder}</option>
                {dashboardTemplates.map((tpl) => (
                  <option key={tpl.id} value={tpl.id} title={tpl.description[locale]} disabled={addedTemplateIds.has(tpl.id)}>
                    {tpl.name[locale]} {addedTemplateIds.has(tpl.id) ? t.dashboard.templateAlreadyAdded : ""}
                  </option>
                ))}
              </select>
              {selectedTemplateId && (
                <p className="max-w-[260px] text-[11px] text-[var(--color-text-muted)]">
                  {dashboardTemplates.find((tpl) => tpl.id === selectedTemplateId)?.description[locale]}
                </p>
              )}
            </div>
            <button
              onClick={addTemplate}
              disabled={!selectedTemplateId || addedTemplateIds.has(selectedTemplateId)}
              className="rounded-md border border-[var(--color-border)] px-3 py-2 text-sm hover:bg-[var(--color-border)]/30 disabled:opacity-60"
            >
              {t.dashboard.addTemplate}
            </button>

            <div className="h-8 w-px bg-[var(--color-border)]" />

            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.panelTitle}</label>
              <input
                value={newPanelTitle}
                onChange={(e) => setNewPanelTitle(e.target.value)}
                className="aruzor-input min-w-[160px]"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.panelMetric}</label>
              <select
                value={newPanelMetric}
                onChange={(e) => setNewPanelMetric(e.target.value)}
                className="aruzor-select min-w-[220px]"
              >
                <option value="">{t.dashboard.panelMetricPlaceholder}</option>
                {metrics.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
              {newPanelMetric && describeMetric(newPanelMetric, locale) && (
                <p className="max-w-[260px] text-xs text-[var(--color-text-muted)]">
                  {describeMetric(newPanelMetric, locale)}
                </p>
              )}
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.panelType}</label>
              <select
                value={newPanelType}
                onChange={(e) => setNewPanelType(e.target.value as PanelType)}
                className="aruzor-select min-w-[130px]"
              >
                {(["line", "area", "bar", "pie", "stat", "gauge"] as PanelType[]).map((pt) => (
                  <option key={pt} value={pt}>
                    {t.dashboard.panelTypeNames[pt]}
                  </option>
                ))}
              </select>
            </div>
            {newPanelType === "line" && (
              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.thresholdLabel}</label>
                <input
                  type="number"
                  value={newPanelThreshold}
                  onChange={(e) => setNewPanelThreshold(e.target.value)}
                  placeholder={t.dashboard.thresholdOptional}
                  className="aruzor-input w-28"
                />
              </div>
            )}
            <button
              onClick={addPanel}
              disabled={!newPanelMetric}
              className="rounded-md bg-[var(--color-primary)] px-3 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
            >
              {t.dashboard.addPanel}
            </button>
          </div>

          <div className="flex flex-wrap items-end gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.annotationLabel}</label>
              <input
                value={newAnnotationLabel}
                onChange={(e) => setNewAnnotationLabel(e.target.value)}
                placeholder={t.dashboard.annotationPlaceholder}
                className="aruzor-input min-w-[180px]"
              />
            </div>
            <button
              onClick={addAnnotation}
              disabled={!newAnnotationLabel.trim()}
              className="rounded-md border border-[var(--color-border)] px-3 py-2 text-sm hover:bg-[var(--color-border)]/30 disabled:opacity-60"
            >
              {t.dashboard.addAnnotation}
            </button>
            {def.annotations.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {def.annotations.map((a) => (
                  <span
                    key={a.id}
                    className="flex items-center gap-1 rounded-full border border-[var(--color-border)] px-2 py-1 text-xs"
                  >
                    {a.label}
                    <button onClick={() => removeAnnotation(a.id)} className="text-[var(--color-danger)]">
                      ✕
                    </button>
                  </span>
                ))}
              </div>
            )}
            <p className="w-full text-xs text-[var(--color-text-muted)]">{t.dashboard.annotationHint}</p>
          </div>

          <div className="flex flex-wrap items-end gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.variableName}</label>
              <input
                value={newVarName}
                onChange={(e) => setNewVarName(e.target.value)}
                placeholder="instance"
                className="aruzor-input min-w-[140px]"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.dashboard.variableLabel}</label>
              <input
                value={newVarLabel}
                onChange={(e) => setNewVarLabel(e.target.value)}
                placeholder="instance"
                className="aruzor-input min-w-[140px]"
              />
            </div>
            <button
              onClick={addVariable}
              disabled={!newVarName.trim() || !newVarLabel.trim()}
              className="rounded-md border border-[var(--color-border)] px-3 py-2 text-sm hover:bg-[var(--color-border)]/30 disabled:opacity-60"
            >
              {t.dashboard.addVariable}
            </button>
            {def.variables.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {def.variables.map((v) => (
                  <span
                    key={v.name}
                    className="flex items-center gap-1 rounded-full border border-[var(--color-border)] px-2 py-1 text-xs"
                  >
                    ${v.name}
                    <button
                      onClick={() => removeVariable(v.name)}
                      title={t.dashboard.removeVariable}
                      className="text-[var(--color-danger)]"
                    >
                      ✕
                    </button>
                  </span>
                ))}
              </div>
            )}
            <p className="w-full text-xs text-[var(--color-text-muted)]">{t.dashboard.variableHint}</p>
          </div>
        </div>
      )}

      {def.panels.length === 0 && (
        <DiscoveryPanel onAdd={addDiscovered} />
      )}

      {/* Above the grid rather than inside it: the grid is PromQL panels the
          operator chose, and traffic numbers come from the access log, not
          from Prometheus. Renders nothing at all when the feature is not set
          up or the signed-in role may not see it. */}
      <TrafficStrip />

      <ReactGridLayout
        layouts={layouts}
        breakpoints={GRID_BREAKPOINTS}
        cols={GRID_COLS}
        rowHeight={70}
        onBreakpointChange={setBreakpoint}
        onLayoutChange={handleLayoutChange}
        isDraggable={editMode && !stacked}
        isResizable={editMode && !stacked}
        draggableCancel=".no-drag"
      >
        {def.panels.map((p) => (
          <div key={p.id} className="relative">
            <DashboardPanel
              title={shownTitles[p.id] ?? p.title}
              duplicate={duplicatePanels.has(p.id)}
              promQL={resolvePromQL(p.promql, varValues)}
              color={p.color}
              panelType={p.panelType}
              threshold={p.threshold}
              annotations={def.annotations}
              rangeSeconds={rangeSeconds}
              refreshMs={refreshMs}
              refreshNonce={refreshNonce}
              instanceFilter={host || undefined}
              editable={editMode}
              onRemove={() => removePanel(p.id)}
              onDuplicate={() => duplicatePanel(p.id)}
              onChangeType={(type) => changePanelType(p.id, type)}
            />
          </div>
        ))}
      </ReactGridLayout>

      <ShortcutsHelp
        open={shortcutsOpen}
        onClose={() => setShortcutsOpen(false)}
        shortcuts={[
          { keys: "r", description: t.dashboard.shortcutRefresh },
          ...(canEdit ? [{ keys: "e", description: t.dashboard.shortcutEdit }] : []),
          { keys: "?", description: t.dashboard.shortcutHelp },
          { keys: "Esc", description: t.dashboard.shortcutEscape },
        ]}
      />

      {dashboardId && (
        <ShareDialog open={shareOpen} dashboardId={dashboardId} onClose={() => setShareOpen(false)} />
      )}

      <ConfirmDialog
        open={confirmingDeleteDashboard}
        title={t.dashboard.confirmDeleteDashboardTitle}
        message={t.dashboard.confirmDeleteDashboardMessage}
        confirmLabel={t.alerts.confirm}
        cancelLabel={t.dashboard.cancel}
        onConfirm={handleDeleteDashboard}
        onCancel={() => setConfirmingDeleteDashboard(false)}
      />
    </div>
  );
}
