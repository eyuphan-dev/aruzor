"use client";

import { useEffect, useState } from "react";
import Image from "next/image";
import { useI18n } from "@/lib/i18n/context";
import { useAuth } from "@/lib/auth/context";
import { completeSetup, getSetupStatus } from "@/lib/api";

const inputClass =
  "aruzor-input w-full transition-colors focus:border-[var(--color-primary)]";

export default function LoginPage() {
  const { t } = useI18n();
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // null while unknown: showing the login form and then swapping it for the
  // setup form would flash the wrong screen at exactly the moment someone is
  // deciding whether the install worked.
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);

  useEffect(() => {
    let cancelled = false;
    getSetupStatus()
      .then((s) => {
        if (!cancelled) setNeedsSetup(s.needsSetup);
      })
      .catch(() => {
        // An older backend has no setup endpoint, and a server that is still
        // starting will not answer either. Both mean "show the login form" —
        // an account almost certainly exists.
        if (!cancelled) setNeedsSetup(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await login(email, password);
    } catch {
      setError(t.login.error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleSetup = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password.length < 8) {
      setError(t.setup.tooShort);
      return;
    }
    if (password !== confirm) {
      setError(t.setup.mismatch);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await completeSetup(email, password);
      // Straight into the app: making someone re-type the credentials they
      // just chose is a step with no purpose.
      await login(email, password);
    } catch (err) {
      setError((err as Error).message || t.setup.error);
      setSubmitting(false);
    }
  };

  return (
    <div className="grid min-h-screen w-full grid-cols-1 lg:grid-cols-2">
      {/* Brand panel — hidden on small screens */}
      <div
        className="relative hidden flex-col items-center justify-center overflow-hidden p-12 lg:flex"
        style={{ background: "radial-gradient(circle at 30% 20%, #0f3d38 0%, #090d16 65%)" }}
      >
        <div
          className="absolute inset-0 opacity-40"
          style={{
            backgroundImage:
              "radial-gradient(circle at 80% 80%, rgba(2,195,154,0.25), transparent 40%), radial-gradient(circle at 15% 70%, rgba(0,168,150,0.2), transparent 35%)",
          }}
        />
        <div className="relative flex flex-col items-center text-center">
          <div className="overflow-hidden rounded-2xl shadow-[0_0_60px_rgba(2,195,154,0.35)]">
            <Image src="/logo-mark.png" alt="Aruzor" width={104} height={104} priority />
          </div>
          <h1 className="mt-6 text-2xl font-semibold tracking-tight text-white">{t.appName}</h1>
          <p className="mt-2 max-w-xs text-sm text-white/60">{t.tagline}</p>
        </div>
      </div>

      {/* Form panel */}
      <div className="flex items-center justify-center p-6">
        {needsSetup === null ? (
          <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
        ) : (
          <form onSubmit={needsSetup ? handleSetup : handleLogin} className="w-full max-w-sm">
            <div className="mb-8 flex items-center gap-2 lg:hidden">
              <Image src="/logo-mark.png" alt="" width={32} height={32} className="rounded-lg" priority />
              <span className="text-lg font-semibold tracking-tight">{t.appName}</span>
            </div>

            <h2 className="text-xl font-semibold">{needsSetup ? t.setup.title : t.login.title}</h2>
            <p className="mt-1 text-sm text-[var(--color-text-muted)]">
              {needsSetup ? t.setup.subtitle : t.login.subtitle}
            </p>

            <div className="mt-6 flex flex-col gap-4">
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--color-text-muted)]">
                  {needsSetup ? t.setup.email : t.login.email}
                </label>
                <input
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className={inputClass}
                  autoComplete="email"
                  autoFocus
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-[var(--color-text-muted)]">
                  {needsSetup ? t.setup.password : t.login.password}
                </label>
                <input
                  type="password"
                  required
                  minLength={needsSetup ? 8 : undefined}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className={inputClass}
                  autoComplete={needsSetup ? "new-password" : "current-password"}
                />
                {needsSetup && (
                  <span className="text-xs text-[var(--color-text-muted)]">{t.setup.passwordHint}</span>
                )}
              </div>

              {needsSetup && (
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.setup.confirm}</label>
                  <input
                    type="password"
                    required
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    className={inputClass}
                    autoComplete="new-password"
                  />
                </div>
              )}
            </div>

            {error && (
              <p className="mt-4 rounded-lg bg-[var(--color-danger)]/10 px-3 py-2 text-sm text-[var(--color-danger)]">
                {error}
              </p>
            )}

            <button
              type="submit"
              disabled={submitting}
              className="mt-6 w-full rounded-lg bg-[var(--color-primary)] px-3 py-2.5 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-60"
            >
              {needsSetup
                ? submitting
                  ? t.setup.submitting
                  : t.setup.submit
                : submitting
                  ? t.login.submitting
                  : t.login.submit}
            </button>

            {needsSetup && (
              <p className="mt-4 text-center text-xs text-[var(--color-text-muted)]">{t.setup.note}</p>
            )}
          </form>
        )}
      </div>
    </div>
  );
}
