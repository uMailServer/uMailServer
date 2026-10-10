import type { ServiceStatus } from "@/types";

/**
 * Derives the dashboard service list from the /health probe. The server only
 * reports HTTP/DB/queue health; SMTP and IMAP listeners have no status source,
 * so they are reported honestly as "unknown" instead of a hard-coded
 * "operational" claim.
 */
export function deriveServiceStatuses(
  health: { ok: boolean; reachable: boolean } | null
): ServiceStatus[] {
  let api: ServiceStatus["status"] = "unknown";
  if (health) {
    api = !health.reachable ? "down" : health.ok ? "operational" : "degraded";
  }
  return [
    { name: "SMTP Server", status: "unknown", port: 25 },
    { name: "IMAP Server", status: "unknown", port: 993 },
    { name: "HTTP API", status: api, port: 8443 },
  ];
}

export async function probeHealth(): Promise<{ ok: boolean; reachable: boolean }> {
  try {
    const res = await fetch("/health", { credentials: "include" });
    return { ok: res.ok, reachable: true };
  } catch {
    return { ok: false, reachable: false };
  }
}

const EMAIL_KEY = "umail_admin_email";

export function rememberEmail(email: string | null): void {
  try {
    if (email) localStorage.setItem(EMAIL_KEY, email);
    else localStorage.removeItem(EMAIL_KEY);
  } catch {
    // storage unavailable
  }
}

export function recalledEmail(): string {
  try {
    return localStorage.getItem(EMAIL_KEY) || "Administrator";
  } catch {
    return "Administrator";
  }
}
