import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { I18nProvider } from "@/lib/i18n/context";
import { ThemeProvider } from "@/lib/theme/context";
import { AuthProvider } from "@/lib/auth/context";
import { AppShell } from "@/components/AppShell";
import { ServiceWorkerRegistrar } from "@/components/ServiceWorkerRegistrar";
import { InstallPrompt } from "@/components/InstallPrompt";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Aruzor — Akıllı Sistem İzleme",
  description: "Prometheus üzerine kurulu, sıfır kod sorgu mimarisine sahip izleme platformu.",
  manifest: "/manifest.json",
  applicationName: "Aruzor",
  appleWebApp: {
    // iOS has no manifest support; these are what let an added-to-home-screen
    // Aruzor open without Safari's chrome.
    capable: true,
    title: "Aruzor",
    statusBarStyle: "black-translucent",
  },
  other: {
    // Next emits the standardised "mobile-web-app-capable". Safari only
    // learned that name recently, so the Apple-prefixed one is kept for
    // iOS versions that still expect it — without it, the app opens inside
    // Safari's chrome instead of standalone.
    "apple-mobile-web-app-capable": "yes",
  },
  icons: {
    icon: [
      { url: "/icon-192.png", sizes: "192x192", type: "image/png" },
      { url: "/icon-512.png", sizes: "512x512", type: "image/png" },
    ],
    apple: "/apple-touch-icon.png",
  },
};

export const viewport: Viewport = {
  // The installed app should reach under the notch and home indicator rather
  // than leaving letterbox bars where the page background stops.
  viewportFit: "cover",
  themeColor: "#020714",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="tr"
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col">
        <ThemeProvider>
          <I18nProvider>
            <AuthProvider>
              <AppShell>{children}</AppShell>
              <ServiceWorkerRegistrar />
              <InstallPrompt />
            </AuthProvider>
          </I18nProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
