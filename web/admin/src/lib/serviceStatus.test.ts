import { describe, it, expect } from "vitest";
import { deriveServiceStatuses } from "./serviceStatus";
import appSrc from "../App.tsx?raw";

describe("F6313 service statuses are derived, never hard-coded operational", () => {
  it("is unknown before probe and for SMTP/IMAP always", () => {
    const s = deriveServiceStatuses(null);
    expect(s.every((x) => x.status === "unknown")).toBe(true);
    const ok = deriveServiceStatuses({ ok: true, reachable: true });
    expect(ok.find((x) => x.name === "SMTP Server")?.status).toBe("unknown");
    expect(ok.find((x) => x.name === "HTTP API")?.status).toBe("operational");
  });
  it("maps failures", () => {
    expect(deriveServiceStatuses({ ok: false, reachable: true })[2].status).toBe("degraded");
    expect(deriveServiceStatuses({ ok: false, reachable: false })[2].status).toBe("down");
  });
  it("App no longer hard-codes admin@example.com", () => {
    expect(appSrc).not.toContain("admin@example.com");
  });
});
