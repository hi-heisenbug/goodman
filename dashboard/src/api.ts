import type { Alert, Fingerprint, Report } from "./types";

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
// enriched with OSV.dev vulnerabilities.
export async function buildReport(lockfile: string, opts?: { service?: string; osv?: boolean }): Promise<Report> {
  const u = new URL("/v1/report", location.origin);
  if (opts?.service) u.searchParams.set("service", opts.service);
  if (opts?.osv) u.searchParams.set("osv", "1");
  const r = await request(u, { method: "POST", body: lockfile });
  if (!r.ok) throw new Error(`report: ${r.status} ${await r.text()}`);
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

// subscribe opens the SSE stream and invokes handlers on each event frame.
// EventSource cannot set headers, so the token rides in a query parameter
// (the collector accepts ?token= on /v1/stream only).
export function subscribe(handlers: {
  onOpen?: () => void;
  onError?: () => void;
  onAlerts?: (a: Alert[]) => void;
  onEvents?: (e: unknown[]) => void;
}): () => void {
  const u = new URL("/v1/stream", location.origin);
  const token = getToken();
  if (token) u.searchParams.set("token", token);
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
