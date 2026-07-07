import { useCallback, useEffect, useMemo, useState } from "react";
import type { Alert, Fingerprint, Severity } from "./types";
import { ackAlert, fetchAlerts, fetchFingerprints, resolveAlert, subscribe } from "./api";

const SENSITIVE = /(secret|token|credential|password|shadow|169\.254\.169\.254|\.pem|\.key|\.aws|\.ssh|\.npmrc|\.env|id_rsa)/i;

function SeverityIcon({ sev }: { sev: Severity }) {
  if (sev === "CRITICAL")
    return (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 2 2 21h20L12 2Z" /><path d="M12 9v5" /><path d="M12 17.5v.2" />
      </svg>
    );
  if (sev === "WARN")
    return (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="12" r="9" /><path d="M12 8v4.5" /><path d="M12 16v.2" />
      </svg>
    );
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="9" /><path d="M12 11v5" /><path d="M12 8v.2" />
    </svg>
  );
}

function relTime(ns: number): string {
  const ms = ns / 1e6;
  const diff = Date.now() - ms;
  if (diff < 60_000) return `${Math.max(1, Math.floor(diff / 1000))}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(ms).toLocaleDateString();
}

function Behavior({ text, kind }: { text: string; kind: "add" | "base" }) {
  const hot = SENSITIVE.test(text);
  return (
    <div className={`behavior ${kind}`}>
      <span className="mark">{kind === "add" ? "+" : "✓"}</span>
      <span className={hot && kind === "add" ? "hi" : undefined}>{text}</span>
    </div>
  );
}

function AlertCard({ a, onChange }: { a: Alert; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const critRule = a.new_behaviors.find((b) => SENSITIVE.test(b));
  const act = async (fn: (id: string) => Promise<void>) => {
    setBusy(true);
    try {
      await fn(a.id);
      onChange();
    } finally {
      setBusy(false);
    }
  };
  const kubectl = `kubectl set image deployment/${a.service} ${a.package}=${a.package}@${a.old_version || "previous"}`;
  return (
    <div className={`alert ${a.severity} ${a.status}`}>
      <div className="head">
        <span className={`badge ${a.severity}`}>
          <SeverityIcon sev={a.severity} /> {a.severity}
        </span>
        <div className="title">
          <div className="pkg">{a.package}</div>
          <div className="sub">
            service <b>{a.service}</b> &middot;{" "}
            <span className="vershift">
              <span className="old">{a.old_version || "—"}</span>
              <span className="arrow">→</span>
              <span className="new">{a.new_version}</span>
            </span>
          </div>
        </div>
        <div className="when">{relTime(a.detected_at)}</div>
      </div>
      <div className="diff">
        <div className="col">
          <h4>
            Baseline behavior <span className="tag b-good">ESTABLISHED</span>
          </h4>
          <Behavior kind="base" text="reads only within its own package dir" />
          <Behavior kind="base" text="known service dependencies" />
        </div>
        <div className="col">
          <h4>
            Drift detected <span className="tag" style={{ color: "var(--critical)" }}>{a.new_behaviors.length} NEW</span>
          </h4>
          {a.new_behaviors.map((b) => (
            <Behavior key={b} kind="add" text={b} />
          ))}
        </div>
      </div>
      <div className="actions">
        <button className="primary" title={kubectl} onClick={() => navigator.clipboard?.writeText(kubectl)}>
          Copy rollback
        </button>
        <a
          href={`https://www.npmjs.com/package/${a.package}/v/${a.new_version}`}
          target="_blank"
          rel="noreferrer"
        >
          Investigate ↗
        </a>
        <span className="spacer" />
        {critRule && <span className="rule">matched: {SENSITIVE.exec(critRule)?.[0]}</span>}
        {a.status === "open" && (
          <button disabled={busy} onClick={() => act(ackAlert)}>
            Acknowledge
          </button>
        )}
        {a.status !== "resolved" && (
          <button disabled={busy} onClick={() => act(resolveAlert)}>
            Resolve
          </button>
        )}
      </div>
    </div>
  );
}

function AlertsView() {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [status, setStatus] = useState<string>("open");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    fetchAlerts(status || undefined)
      .then((a) => {
        setAlerts(a);
        setErr("");
      })
      .catch((e) => setErr(String(e)));
  }, [status]);

  useEffect(() => {
    load();
    const unsub = subscribe({ onAlerts: () => load() });
    const t = setInterval(load, 5000);
    return () => {
      unsub();
      clearInterval(t);
    };
  }, [load]);

  return (
    <>
      {err && <div className="err">{err}</div>}
      <div className="filters">
        <div className="seg">
          {["open", "acknowledged", "resolved", ""].map((s) => (
            <button key={s || "all"} className={status === s ? "active" : ""} onClick={() => setStatus(s)}>
              {s || "all"}
            </button>
          ))}
        </div>
      </div>
      {alerts.length === 0 ? (
        <div className="empty">
          <div className="big">🛡️</div>
          No {status || ""} alerts. Dependencies are behaving within baseline.
        </div>
      ) : (
        alerts.map((a) => <AlertCard key={a.id} a={a} onChange={load} />)
      )}
    </>
  );
}

function FingerprintsView() {
  const [fps, setFps] = useState<Fingerprint[]>([]);
  const [q, setQ] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    fetchFingerprints()
      .then((f) => {
        setFps(f);
        setErr("");
      })
      .catch((e) => setErr(String(e)));
  }, []);

  const filtered = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return fps;
    return fps.filter((f) => f.package.toLowerCase().includes(t) || f.service.toLowerCase().includes(t));
  }, [fps, q]);

  return (
    <>
      {err && <div className="err">{err}</div>}
      <div className="filters">
        <input placeholder="search package or service…" value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      {filtered.length === 0 ? (
        <div className="empty">
          <div className="big">🔍</div>
          No fingerprints learned yet. Run a workload to populate baselines.
        </div>
      ) : (
        filtered.map((f) => <FingerprintCard key={`${f.service}/${f.package}/${f.version}`} fp={f} />)
      )}
    </>
  );
}

function FingerprintCard({ fp }: { fp: Fingerprint }) {
  const entries = Object.entries(fp.behaviors).sort((a, b) => b[1].count - a[1].count);
  const max = Math.max(1, ...entries.map(([, s]) => s.count));
  return (
    <div className="fp">
      <div className="fhead">
        <span className="fpkg">
          {fp.package}
          {fp.version ? `@${fp.version}` : ""}
        </span>
        <span className={`chip ${fp.is_baseline ? "base" : "learn"}`}>
          {fp.is_baseline ? "BASELINE" : "LEARNING"}
        </span>
        <span className="meta">
          {fp.service} &middot; {fp.obs_count} obs &middot; {entries.length} behaviors
        </span>
      </div>
      {entries.map(([name, s]) => (
        <div className="bhrow" key={name}>
          <span className="name">{name}</span>
          <span className="bar" style={{ width: `${Math.max(4, (s.count / max) * 160)}px` }} />
          <span className="cnt">×{s.count}</span>
        </div>
      ))}
    </div>
  );
}

export function App() {
  const [tab, setTab] = useState<"alerts" | "fingerprints">("alerts");
  const [openCount, setOpenCount] = useState(0);
  const [live, setLive] = useState(false);

  useEffect(() => {
    const refresh = () => fetchAlerts("open").then((a) => setOpenCount(a.length)).catch(() => {});
    refresh();
    const unsub = subscribe({
      onAlerts: () => {
        setLive(true);
        refresh();
      },
      onEvents: () => setLive(true),
    });
    const t = setInterval(refresh, 5000);
    return () => {
      unsub();
      clearInterval(t);
    };
  }, []);

  return (
    <div className="app">
      <header className="top">
        <div className="logo">G</div>
        <div className="brand">
          <h1>Goodman</h1>
          <p>Runtime dependency behavior monitoring</p>
        </div>
        <span className={`live-dot ${live ? "on" : "off"}`}>
          <i /> {live ? "live" : "connecting"}
        </span>
      </header>

      <div className="tabs">
        <button className={tab === "alerts" ? "active" : ""} onClick={() => setTab("alerts")}>
          Alerts
          {openCount > 0 && <span className="count">{openCount}</span>}
        </button>
        <button className={tab === "fingerprints" ? "active" : ""} onClick={() => setTab("fingerprints")}>
          Fingerprint Explorer
        </button>
      </div>

      {tab === "alerts" ? <AlertsView /> : <FingerprintsView />}
    </div>
  );
}
