"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import Image from "next/image";
import { usePathname } from "next/navigation";
import { useI18n } from "@/lib/i18n/context";
import { useTheme } from "@/lib/theme/context";
import { useAuth } from "@/lib/auth/context";
import { NavIcon, type NavKey } from "./NavIcon";
import { OfflineBanner } from "./OfflineBanner";
import { PushToggle } from "./PushToggle";

type NavItem = { href: string; key: NavKey };

const navItems: NavItem[] = [
  { href: "/", key: "dashboard" },
  { href: "/query-builder", key: "queryBuilder" },
  { href: "/alerts", key: "alerts" },
  { href: "/datasources", key: "datasources" },
  { href: "/monitors", key: "monitors" },
];

// A phone shows four destinations plus More. Beyond five a tab stops being
// comfortably tappable at 360px, and these four are the ones opened daily —
// the rest is configuration you set once and forget.
const TAB_BAR: { href: string; key: NavKey & ("dashboard" | "queryBuilder" | "alerts" | "monitors") }[] = [
  { href: "/", key: "dashboard" },
  { href: "/query-builder", key: "queryBuilder" },
  { href: "/alerts", key: "alerts" },
  { href: "/monitors", key: "monitors" },
];

const COLLAPSE_STORAGE_KEY = "aruzor.sidebar.collapsed";

export function AppShell({ children }: { children: React.ReactNode }) {
  const { t, locale, setLocale } = useI18n();
  const { theme, toggleTheme } = useTheme();
  const { session, logout } = useAuth();
  const pathname = usePathname();

  // Collapsing is a per-person habit, so it outlives the session. Read on
  // mount rather than during render to keep the server-rendered markup and
  // the first client paint identical.
  const [collapsed, setCollapsed] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);

  useEffect(() => {
    // The preference only exists in localStorage, so the server-rendered
    // expanded default is corrected once after mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCollapsed(window.localStorage.getItem(COLLAPSE_STORAGE_KEY) === "1");
  }, []);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      localStorage.setItem(COLLAPSE_STORAGE_KEY, prev ? "0" : "1");
      return !prev;
    });
  };

  useEffect(() => {
    if (!moreOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMoreOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [moreOpen]);

  // The sheet sits over the page. Without this the page keeps scrolling
  // under the finger behind it, which no native sheet does.
  useEffect(() => {
    if (!moreOpen) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [moreOpen]);

  // Share links render their own header. Without this, a logged-in person
  // opening one would see it wrapped in the app chrome while everyone else
  // sees a bare page — the same URL should look the same to everybody.
  if (pathname === "/login" || pathname.startsWith("/s/") || !session) {
    return <div className="min-h-screen w-full">{children}</div>;
  }

  const items: NavItem[] =
    session.role === "super_admin"
      ? [
          ...navItems,
          { href: "/users", key: "users" },
          { href: "/logs", key: "logs" },
          { href: "/settings", key: "settings" },
        ]
      : navItems;

  // Whatever the tab bar cannot show goes in the sheet, so no destination is
  // unreachable on a phone whichever role is signed in.
  const tabHrefs = new Set(TAB_BAR.map((i) => i.href));
  const overflowItems = items.filter((i) => !tabHrefs.has(i.href));

  // A detail page under a section is still that section: /monitors/<id>
  // has to keep the Monitors tab lit and the title bar naming it, or the
  // app looks like it navigated somewhere it does not know about.
  const isActive = (href: string) => pathname === href || (href !== "/" && pathname.startsWith(href + "/"));

  const activeItem = items.find((i) => isActive(i.href));
  const pageTitle = activeItem ? t.nav[activeItem.key] : t.appName;
  const moreActive = overflowItems.some((i) => isActive(i.href));

  const sidebar = (railed: boolean) => (
    <>
      <div className={`flex items-center gap-2 py-5 ${railed ? "justify-center px-2" : "px-5"}`}>
        <Image src="/logo-mark.png" alt="" width={28} height={28} className="rounded-md" priority />
        {!railed && <span className="text-lg font-semibold tracking-tight">{t.appName}</span>}
      </div>
      <nav className={`flex flex-1 flex-col gap-1 ${railed ? "px-2" : "px-3"}`}>
        {items.map((item) => {
          const active = isActive(item.href);
          return (
            <Link
              key={item.href}
              href={item.href}
              title={railed ? t.nav[item.key] : undefined}
              aria-current={active ? "page" : undefined}
              className={`aruzor-nav-link flex items-center gap-3 rounded-lg py-2 text-sm font-medium transition-colors ${
                railed ? "justify-center px-2" : "px-3"
              } ${
                active
                  ? "bg-[var(--color-primary)]/15 text-[var(--color-primary)]"
                  : "text-[var(--color-text-muted)] hover:bg-[var(--color-border)]/50 hover:text-[var(--color-text)]"
              }`}
            >
              <NavIcon name={item.key} />
              {!railed && <span className="truncate">{t.nav[item.key]}</span>}
            </Link>
          );
        })}
      </nav>
      {!railed && <div className="px-5 py-4 text-xs text-[var(--color-text-muted)]">{t.tagline}</div>}
    </>
  );

  const accountControls = (
    <div className="flex flex-wrap items-center gap-2">
      <PushToggle />
      <button
        onClick={() => setLocale(locale === "tr" ? "en" : "tr")}
        className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs font-medium uppercase tracking-wide text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
        aria-label={t.common.language}
      >
        {locale}
      </button>
      <button
        onClick={toggleTheme}
        className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs font-medium text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
        aria-label={t.common.theme}
      >
        {theme === "dark" ? "🌙" : "☀️"}
      </button>
      <button
        onClick={logout}
        className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs font-medium text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
      >
        {t.logout}
      </button>
    </div>
  );

  return (
    <div className="flex h-screen w-full overflow-hidden">
      <aside
        className={`aruzor-app-rail hidden shrink-0 flex-col overflow-y-auto border-r border-[var(--color-border)] bg-[var(--color-card)] transition-[width] duration-200 md:flex ${
          collapsed ? "w-16" : "w-60"
        }`}
      >
        {sidebar(collapsed)}
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <OfflineBanner />

        {/* Below md this is a title bar the way an app has one: it names the
            destination you are on. Navigation lives in the tab bar and the
            account controls in the sheet behind More. */}
        <header className="aruzor-app-header aruzor-chrome flex items-center justify-between gap-2 border-b border-[var(--color-border)] bg-[var(--color-card)]">
          <button
            onClick={toggleCollapsed}
            title={collapsed ? t.common.expandMenu : t.common.collapseMenu}
            aria-label={collapsed ? t.common.expandMenu : t.common.collapseMenu}
            aria-expanded={!collapsed}
            className="-ml-2 hidden rounded-md p-2 text-[var(--color-text-muted)] hover:text-[var(--color-text)] md:block"
          >
            <MenuIcon />
          </button>
          {/* Below md the rail is gone, so this is the only place the mark
              appears. Using the installed app icon rather than the wordmark
              ties what is in the title bar to what is on the home screen. */}
          <div className="flex min-w-0 items-center gap-2 md:hidden">
            <Image src="/icon-192.png" alt="" width={26} height={26} className="shrink-0 rounded-md" priority />
            <h1 className="truncate text-[17px] font-semibold tracking-tight">{pageTitle}</h1>
          </div>
          <div className="ml-auto hidden items-center gap-2 md:flex">
            <span className="text-xs text-[var(--color-text-muted)]">{session.email}</span>
            {accountControls}
          </div>
        </header>

        <main className="aruzor-app-main flex-1 overflow-y-auto">{children}</main>

        {/* A flex sibling rather than a fixed overlay: the bar then takes its
            own space instead of covering the last panel on the page, so no
            page has to reserve padding for it. */}
        <nav
          aria-label={t.common.moreTitle}
          className="aruzor-tabbar aruzor-chrome flex shrink-0 items-stretch border-t border-[var(--color-border)] bg-[var(--color-card)] md:hidden"
        >
          {TAB_BAR.map((item) => {
            const active = isActive(item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                aria-current={active ? "page" : undefined}
                className={`aruzor-tab ${active ? "text-[var(--color-primary)]" : "text-[var(--color-text-muted)]"}`}
              >
                <NavIcon name={item.key} size={22} />
                <span className="aruzor-tab-label">{t.navShort[item.key]}</span>
              </Link>
            );
          })}
          <button
            onClick={() => setMoreOpen(true)}
            aria-expanded={moreOpen}
            className={`aruzor-tab ${moreActive ? "text-[var(--color-primary)]" : "text-[var(--color-text-muted)]"}`}
          >
            <MoreIcon />
            <span className="aruzor-tab-label">{t.common.more}</span>
          </button>
        </nav>
      </div>

      {moreOpen && (
        <div className="fixed inset-0 z-40 md:hidden">
          <button
            className="absolute inset-0 bg-black/50"
            aria-label={t.common.close}
            onClick={() => setMoreOpen(false)}
          />
          <div className="aruzor-sheet absolute inset-x-0 bottom-0 max-h-[85vh] overflow-y-auto rounded-t-2xl border-t border-[var(--color-border)] bg-[var(--color-card)]">
            {/* The grab handle is what tells someone at a glance that this
                panel came up from the bottom and goes back down. */}
            <div className="flex justify-center pt-2 pb-1">
              <span className="h-1 w-10 rounded-full bg-[var(--color-border)]" />
            </div>
            <div className="flex items-center gap-3 px-5 pb-2">
              <Image src="/icon-192.png" alt="" width={36} height={36} className="shrink-0 rounded-lg" />
              <div className="min-w-0">
                <p className="text-base font-semibold">{t.appName}</p>
                <p className="truncate text-xs text-[var(--color-text-muted)]">{session.email}</p>
              </div>
            </div>
            <div className="flex flex-col px-3 pb-2">
              {overflowItems.map((item) => {
                const active = isActive(item.href);
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    aria-current={active ? "page" : undefined}
                    onClick={() => setMoreOpen(false)}
                    className={`aruzor-nav-link flex items-center gap-3 rounded-xl px-3 py-3 text-[15px] font-medium ${
                      active
                        ? "bg-[var(--color-primary)]/15 text-[var(--color-primary)]"
                        : "text-[var(--color-text)]"
                    }`}
                  >
                    <NavIcon name={item.key} size={20} />
                    <span className="truncate">{t.nav[item.key]}</span>
                  </Link>
                );
              })}
            </div>
            <div className="border-t border-[var(--color-border)] px-5 py-4">
              <p className="mb-2 text-xs font-medium uppercase tracking-wide text-[var(--color-text-muted)]">
                {t.common.account}
              </p>
              {accountControls}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function MenuIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
      <path d="M4 7h16M4 12h16M4 17h16" />
    </svg>
  );
}

function MoreIcon() {
  return (
    <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true">
      <circle cx="5" cy="12" r="1.5" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none" />
      <circle cx="19" cy="12" r="1.5" fill="currentColor" stroke="none" />
    </svg>
  );
}
