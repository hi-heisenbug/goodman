import type { Alert, Fingerprint } from "./types";

export async function fetchAlerts(status?: string): Promise<Alert[]> {
  const u = new URL("/v1/alerts", location.origin);
  if (status) u.searchParams.set("status", status);
  const r = await fetch(u);
  if (!r.ok) throw new Error(`alerts: ${r.status}`);
  return r.json();
}

export async function fetchFingerprints(service?: string, pkg?: string): Promise<Fingerprint[]> {
  const u = new URL("/v1/fingerprints", location.origin);
  if (service) u.searchParams.set("service", service);
  if (pkg) u.searchParams.set("package", pkg);
  const r = await fetch(u);
  if (!r.ok) throw new Error(`fingerprints: ${r.status}`);
  return r.json();
}

export async function ackAlert(id: string): Promise<void> {
  const r = await fetch(`/v1/alerts/${encodeURIComponent(id)}/ack`, { method: "POST" });
  if (!r.ok) throw new Error(`ack: ${r.status}`);
}

export async function resolveAlert(id: string): Promise<void> {
  const r = await fetch(`/v1/alerts/${encodeURIComponent(id)}/resolve`, { method: "POST" });
  if (!r.ok) throw new Error(`resolve: ${r.status}`);
}

// subscribe opens the SSE stream and invokes handlers on each event frame.
export function subscribe(handlers: {
  onOpen?: () => void;
  onError?: () => void;
  onAlerts?: (a: Alert[]) => void;
  onEvents?: (e: unknown[]) => void;
}): () => void {
  const es = new EventSource("/v1/stream");
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
