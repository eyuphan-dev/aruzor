// crypto.randomUUID() is only exposed in secure contexts (HTTPS or
// localhost); this app is currently served over plain HTTP on some
// deployments, where calling it throws. genId() works everywhere.
export function genId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
