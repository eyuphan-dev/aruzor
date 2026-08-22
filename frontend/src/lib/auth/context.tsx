"use client";

import { createContext, useContext, useEffect, useState } from "react";
import { useRouter, usePathname } from "next/navigation";
import { login as apiLogin } from "@/lib/api";

const STORAGE_KEY = "aruzor-auth";

type Session = { token: string; userId: string; email: string; role: string };

type AuthContextValue = {
  session: Session | null;
  ready: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

const PUBLIC_PATHS = new Set(["/login", "/status"]);

// Share links live under /s/<token> and are meant to be opened by people
// who have no account at all. A prefix rather than an exact path because the
// token is part of the URL.
const PUBLIC_PREFIXES = ["/s/"];

function isPublicPath(pathname: string): boolean {
  return PUBLIC_PATHS.has(pathname) || PUBLIC_PREFIXES.some((p) => pathname.startsWith(p));
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [ready, setReady] = useState(false);
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    // Session only exists in localStorage, so it's read once after mount.
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored) {
      try {
        // eslint-disable-next-line react-hooks/set-state-in-effect
        setSession(JSON.parse(stored));
      } catch {
        window.localStorage.removeItem(STORAGE_KEY);
      }
    }
    setReady(true);
  }, []);

  useEffect(() => {
    if (!ready) return;
    if (!session && !isPublicPath(pathname)) {
      router.replace("/login");
    }
    if (session && pathname === "/login") {
      router.replace("/");
    }
  }, [ready, session, pathname, router]);

  const login = async (email: string, password: string) => {
    const res = await apiLogin(email, password);
    const next: Session = { token: res.token, userId: res.userId, email: res.email, role: res.role };
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    setSession(next);
  };

  const logout = () => {
    window.localStorage.removeItem(STORAGE_KEY);
    setSession(null);
    router.replace("/login");
  };

  return <AuthContext.Provider value={{ session, ready, login, logout }}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
