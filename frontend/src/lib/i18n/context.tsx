"use client";

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { dictionaries, type Locale } from "./dictionaries";

const STORAGE_KEY = "aruzor-locale";

type I18nContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: typeof dictionaries.tr;
};

const I18nContext = createContext<I18nContextValue | null>(null);

function detectLocale(): Locale {
  if (typeof window === "undefined") return "tr";
  const stored = window.localStorage.getItem(STORAGE_KEY) as Locale | null;
  if (stored === "tr" || stored === "en") return stored;
  return navigator.language.toLowerCase().startsWith("tr") ? "tr" : "en";
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>("tr");

  useEffect(() => {
    // Locale is only knowable client-side (localStorage/navigator), so the
    // server-rendered "tr" default is corrected once after mount.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLocaleState(detectLocale());
  }, []);

  const setLocale = (next: Locale) => {
    setLocaleState(next);
    window.localStorage.setItem(STORAGE_KEY, next);
    document.documentElement.lang = next;
  };

  const value = useMemo(
    () => ({ locale, setLocale, t: dictionaries[locale] }),
    [locale],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within I18nProvider");
  return ctx;
}
