import type { Alert, CoverageSnapshot, Fingerprint, Report, StoredReport } from "./types";

// The collector can require a bearer token on the API (GOODMAN_API_TOKEN).
// The token is kept in localStorage; a 401 triggers the registered handler so
// the app can show the token gate.
const TOKEN_KEY = "goodman.apiToken";

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setToken(token: string): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* storage unavailable: token lives for this page only */
  }
  setStreamTokenCookie(token);
}

function setStreamTokenCookie(token: string): void {
  const secure = location.protocol === "https:" ? "; Secure" : "";
  if (!token) {
    document.cookie = `goodman_stream_token=; Path=/v1/stream; SameSite=Strict; Max-Age=0${secure}`;
    return;
  }
  document.cookie = `goodman_stream_token=${encodeURIComponent(token)}; Path=/v1/stream; SameSite=Strict${secure}`;
}

let unauthorizedHandler: (() => void) | undefined;
export function onUnauthorized(handler: () => void): void {
  unauthorizedHandler = handler;
}

async function request(input: string | URL, init?: RequestInit): Promise<Response> {
  const token = getToken();
  const headers = new Headers(init?.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const r = await fetch(input, { ...init, headers });
  if (r.status === 401) {
    unauthorizedHandler?.();
    throw new Error("unauthorized: API token required");
  }
  return r;
}

export async function fetchAlerts(status?: string): Promise<Alert[]> {
  const u = new URL("/v1/alerts", location.origin);
  if (status) u.searchParams.set("status", status);
  u.searchParams.set("limit", "500");
  const r = await request(u);
  if (!r.ok) throw new Error(`alerts: ${r.status}`);
  return r.json();
}

export async function fetchFingerprints(service?: string, pkg?: string): Promise<Fingerprint[]> {
  const u = new URL("/v1/fingerprints", location.origin);
  if (service) u.searchParams.set("service", service);
  if (pkg) u.searchParams.set("package", pkg);
  const r = await request(u);
  if (!r.ok) throw new Error(`fingerprints: ${r.status}`);
  return r.json();
}

// buildReport uploads a package-lock.json and returns the reachability report:
// declared dependencies joined with what Goodman observed executing, optionally
// enriched with OSV.dev vulnerabilities. persist stores the lockfile and
// snapshot so the collector can refresh it and future loads are instant.
export async function buildReport(
  lockfile: string,
  opts?: { service?: string; osv?: boolean; persist?: boolean },
): Promise<Report> {
  const u = new URL("/v1/report", location.origin);
  if (opts?.service) u.searchParams.set("service", opts.service);
  if (opts?.osv) u.searchParams.set("osv", "1");
  if (opts?.persist) u.searchParams.set("persist", "1");
  const r = await request(u, { method: "POST", body: lockfile });
  if (!r.ok) throw new Error(`report: ${r.status} ${await r.text()}`);
  return r.json();
}

// fetchStoredReport returns the most recent persisted reachability snapshot for
// a service scope, or null when none has been uploaded yet.
export async function fetchStoredReport(service?: string): Promise<StoredReport | null> {
  const u = new URL("/v1/report", location.origin);
  if (service) u.searchParams.set("service", service);
  const r = await request(u);
  if (r.status === 404) return null;
  if (!r.ok) throw new Error(`stored report: ${r.status}`);
  return r.json();
}

export async function fetchCoverage(): Promise<CoverageSnapshot> {
  const r = await request("/v1/coverage");
  if (!r.ok) throw new Error(`coverage: ${r.status}`);
  return r.json();
}

export async function ackAlert(id: string): Promise<void> {
  const r = await request(`/v1/alerts/${encodeURIComponent(id)}/ack`, { method: "POST" });
  if (!r.ok) throw new Error(`ack: ${r.status}`);
}

export async function resolveAlert(id: string): Promise<void> {
  const r = await request(`/v1/alerts/${encodeURIComponent(id)}/resolve`, { method: "POST" });
  if (!r.ok) throw new Error(`resolve: ${r.status}`);
}

// subscribe opens the single app-level SSE stream and invokes handlers on each
// event frame. EventSource cannot set headers, so the token is mirrored into a
// SameSite, path-scoped cookie instead of a proxy-visible query parameter.
export function subscribe(handlers: {
  onOpen?: () => void;
  onError?: () => void;
  onAlerts?: (a: Alert[]) => void;
  onEvents?: (e: unknown[]) => void;
}): () => void {
  const u = new URL("/v1/stream", location.origin);
  const token = getToken();
  setStreamTokenCookie(token);
  const es = new EventSource(u);
  if (handlers.onOpen) es.addEventListener("open", handlers.onOpen);
  if (handlers.onError) es.addEventListener("error", handlers.onError);
  if (handlers.onAlerts) {
    es.addEventListener("alerts", (ev) => {
      try {
        handlers.onAlerts!(JSON.parse((ev as MessageEvent).data));
      } catch {
        /* ignore malformed frame */
      }
    });
  }
  if (handlers.onEvents) {
    es.addEventListener("events", (ev) => {
      try {
        handlers.onEvents!(JSON.parse((ev as MessageEvent).data));
      } catch {
        /* ignore */
      }
    });
  }
  return () => es.close();
}
