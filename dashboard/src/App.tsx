import { useCallback, useEffect, useMemo, useState } from "react";
import type { Alert, AlertStatus, Fingerprint, Severity } from "./types";
import { ackAlert, fetchAlerts, fetchFingerprints, getToken, onUnauthorized, resolveAlert, setToken, subscribe } from "./api";

type Tab = "alerts" | "fingerprints";
type Tone = "critical" | "warning" | "good" | "accent" | "neutral";
type IconName =
  | "activity"
  | "alert"
  | "archive"
  | "bolt"
  | "check"
  | "chevronRight"
  | "clipboard"
  | "cube"
  | "fingerprint"
  | "grid"
  | "link"
  | "lock"
  | "search"
  | "shield"
  | "spark"
  | "triangle";

const SENSITIVE =
  /(secret|token|credential|password|shadow|169\.254\.169\.254|\.pem|\.key|\.aws|\.ssh|\.npmrc|\.env|id_rsa)/i;

const STATUS_LABELS: Record<AlertStatus | "all", string> = {
  open: "Open",
  acknowledged: "Acknowledged",
  resolved: "Resolved",
  all: "All",
};

const SEVERITY_LABELS: Record<Severity, string> = {
  CRITICAL: "Critical",
  WARN: "Warning",
  INFO: "Info",
};

const TONE_LABELS: Record<Tone, string> = {
  critical: "Critical",
  warning: "Warning",
  good: "Healthy",
  accent: "Observed",
  neutral: "Neutral",
};

function Icon({ name }: { name: IconName }) {
  const paths: Record<IconName, JSX.Element> = {
    activity: <><path d="M3 12h4l2-7 4 14 2-7h6" /></>,
    alert: <><path d="M12 2 2 21h20L12 2Z" /><path d="M12 9v5" /><path d="M12 17.5h.01" /></>,
    archive: <><path d="M4 7h16" /><path d="M6 7v13h12V7" /><path d="M9 11h6" /><path d="M5 3h14l1 4H4l1-4Z" /></>,
    bolt: <><path d="m13 2-8 12h6l-1 8 8-12h-6l1-8Z" /></>,
    check: <><path d="m5 12 4 4L19 6" /></>,
    chevronRight: <><path d="m9 6 6 6-6 6" /></>,
    clipboard: <><path d="M9 4h6l1 2h3v14H5V6h3l1-2Z" /><path d="M9 10h6" /><path d="M9 14h5" /></>,
    cube: <><path d="m12 2 8 4.5v9L12 20l-8-4.5v-9L12 2Z" /><path d="M4 6.5 12 11l8-4.5" /><path d="M12 11v9" /></>,
    fingerprint: <><path d="M7 7a5 5 0 0 1 10 0" /><path d="M6 11a6 6 0 0 0 12 0" /><path d="M9 11a3 3 0 0 0 6 0V8a3 3 0 0 0-6 0v3Z" /><path d="M12 17v3" /></>,
    grid: <><path d="M4 4h7v7H4z" /><path d="M13 4h7v7h-7z" /><path d="M4 13h7v7H4z" /><path d="M13 13h7v7h-7z" /></>,
    link: <><path d="M10 13a5 5 0 0 0 7.1 0l2-2a5 5 0 0 0-7.1-7.1l-1 1" /><path d="M14 11a5 5 0 0 0-7.1 0l-2 2A5 5 0 0 0 12 20.1l1-1" /></>,
    lock: <><path d="M6 10h12v10H6z" /><path d="M8 10V7a4 4 0 0 1 8 0v3" /></>,
    search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></>,
    shield: <><path d="M12 2 20 5v6c0 5-3.4 8.4-8 11-4.6-2.6-8-6-8-11V5l8-3Z" /><path d="m9 12 2 2 4-5" /></>,
    spark: <><path d="M12 2v6" /><path d="M12 16v6" /><path d="M4.9 4.9 9 9" /><path d="m15 15 4.1 4.1" /><path d="M2 12h6" /><path d="M16 12h6" /><path d="m4.9 19.1 4.1-4.1" /><path d="m15 9 4.1-4.1" /></>,
    triangle: <><path d="M12 3 3 21h18L12 3Z" /></>,
  };

  return (
    <svg className="icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      {paths[name]}
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

function classForSeverity(sev: Severity): string {
  return sev.toLowerCase();
}

function versionShift(a: Alert) {
  return `${a.old_version || "previous"} -> ${a.new_version || "unknown"}`;
}

function pct(value: number, total: number): number {
  if (total <= 0) return 0;
  return Math.round((value / total) * 100);
}

function Behavior({ text, kind }: { text: string; kind: "add" | "base" }) {
  const sensitive = SENSITIVE.test(text);
  return (
    <div className={`behavior ${kind} ${sensitive ? "sensitive" : ""}`}>
      <span className="behavior-mark" aria-hidden="true">
        {kind === "add" ? "+" : "-"}
      </span>
      <span>{text}</span>
    </div>
  );
}

function MetricCard({
  label,
  value,
  tone = "neutral",
  icon,
  detail,
}: {
  label: string;
  value: string | number;
  tone?: "neutral" | "critical" | "warning" | "good" | "accent";
  icon: IconName;
  detail?: string;
}) {
  return (
    <div className={`metric ${tone}`}>
      <div className="metric-icon">
        <Icon name={icon} />
      </div>
      <div>
        <div className="metric-value">{value}</div>
        <div className="metric-label">{label}</div>
        {detail && <div className="metric-detail">{detail}</div>}
      </div>
    </div>
  );
}

function StatusMeter({
  label,
  value,
  total,
  tone,
}: {
  label: string;
  value: number;
  total: number;
  tone: Tone;
}) {
  const width = total > 0 && value > 0 ? Math.max(4, (value / total) * 100) : 0;
  return (
    <div className="status-meter">
      <div className="status-meter-head">
        <span>{label}</span>
        <b>{value}</b>
      </div>
      <div className="status-track" aria-label={`${label}: ${value}`}>
        <i className={tone} style={{ width: `${width}%` }} />
      </div>
    </div>
  );
}

function InsightPanel({
  eyebrow,
  title,
  value,
  tone,
  children,
}: {
  eyebrow: string;
  title: string;
  value: string;
  tone: Tone;
  children: React.ReactNode;
}) {
  return (
    <section className={`insight-panel ${tone}`}>
      <div className="panel-kicker">
        <span>{eyebrow}</span>
        <b>{TONE_LABELS[tone]}</b>
      </div>
      <div className="panel-main">
        <div>
          <h3>{title}</h3>
          <p>{children}</p>
        </div>
        <strong>{value}</strong>
      </div>
    </section>
  );
}

function EmptyState({ icon, title, body }: { icon: IconName; title: string; body: string }) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <Icon name={icon} />
      </div>
      <h3>{title}</h3>
      <p>{body}</p>
    </div>
  );
}

function AlertCard({ alert, onChange }: { alert: Alert; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const baseline = alert.baseline_behaviors || [];
  const criticalBehavior = alert.new_behaviors.find((behavior) => SENSITIVE.test(behavior));
  const rollback = `kubectl set image deployment/${alert.service} ${alert.package}=${alert.package}@${alert.old_version || "previous"}`;

  const act = async (fn: (id: string) => Promise<void>) => {
    setBusy(true);
    try {
      await fn(alert.id);
      onChange();
    } finally {
      setBusy(false);
    }
  };

  const copyRollback = async () => {
    await navigator.clipboard?.writeText(rollback);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  };

  return (
    <article className={`alert-card ${classForSeverity(alert.severity)} ${alert.status}`}>
      <div className="alert-top">
        <div className={`severity-pill ${classForSeverity(alert.severity)}`}>
          <SeverityIcon sev={alert.severity} />
          <span>{SEVERITY_LABELS[alert.severity]}</span>
        </div>

        <div className="alert-title">
          <div className="package-line">
            <span>{alert.package}</span>
            <span className="version-shift">{versionShift(alert)}</span>
          </div>
          <div className="alert-subtitle">
            <span>{alert.service}</span>
            <span>{relTime(alert.detected_at)}</span>
            <span className={`status-dot ${alert.status}`}>{STATUS_LABELS[alert.status]}</span>
          </div>
        </div>

        {criticalBehavior && (
          <div className="risk-chip">
            <Icon name="lock" />
            <span>{SENSITIVE.exec(criticalBehavior)?.[0]}</span>
          </div>
        )}
      </div>

      <div className="diff-grid">
        <section>
          <div className="section-label">
            <span>Known baseline</span>
            <b>{baseline.length}</b>
          </div>
          {baseline.length === 0 ? (
            <p className="quiet">Baseline fingerprint exists, but detailed behavior context is unavailable.</p>
          ) : (
            baseline.map((behavior) => <Behavior key={behavior} kind="base" text={behavior} />)
          )}
        </section>

        <section>
          <div className="section-label critical-text">
            <span>New behavior</span>
            <b>{alert.new_behaviors.length}</b>
          </div>
          {alert.new_behaviors.map((behavior) => (
            <Behavior key={behavior} kind="add" text={behavior} />
          ))}
        </section>
      </div>

      <div className="action-bar">
        <button className="primary-action" title={rollback} onClick={copyRollback}>
          <Icon name={copied ? "check" : "clipboard"} />
          {copied ? "Copied" : "Rollback"}
        </button>
        <a className="secondary-action" href={`https://www.npmjs.com/package/${alert.package}/v/${alert.new_version}`} target="_blank" rel="noreferrer">
          <Icon name="link" />
          Investigate
        </a>
        <span className="action-spacer" />
        {alert.status === "open" && (
          <button className="quiet-action" disabled={busy} onClick={() => act(ackAlert)}>
            Acknowledge
          </button>
        )}
        {alert.status !== "resolved" && (
          <button className="quiet-action" disabled={busy} onClick={() => act(resolveAlert)}>
            Resolve
          </button>
        )}
      </div>
    </article>
  );
}

function SeverityIcon({ sev }: { sev: Severity }) {
  if (sev === "CRITICAL") return <Icon name="alert" />;
  if (sev === "WARN") return <Icon name="bolt" />;
  return <Icon name="activity" />;
}

function AlertsView() {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const getInitialStatus = (): AlertStatus | "all" => {
    const value = new URLSearchParams(window.location.search).get("status");
    return value === "acknowledged" || value === "resolved" || value === "all" ? value : "open";
  };
  const [status, setStatus] = useState<AlertStatus | "all">(getInitialStatus);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    fetchAlerts()
      .then((items) => {
        setAlerts(items);
        setErr("");
      })
      .catch((e) => setErr(String(e)));
  }, []);

  useEffect(() => {
    load();
    if (window.location.search.includes("static")) {
      return;
    }
    const unsub = subscribe({ onAlerts: () => load(), onEvents: () => load() });
    const timer = setInterval(load, 5000);
    return () => {
      unsub();
      clearInterval(timer);
    };
  }, [load]);

  const visible = useMemo(() => {
    if (status === "all") return alerts;
    return alerts.filter((alert) => alert.status === status);
  }, [alerts, status]);

  const counts = useMemo(
    () => ({
      critical: alerts.filter((alert) => alert.severity === "CRITICAL" && alert.status !== "resolved").length,
      warning: alerts.filter((alert) => alert.severity === "WARN" && alert.status !== "resolved").length,
      info: alerts.filter((alert) => alert.severity === "INFO" && alert.status !== "resolved").length,
      open: alerts.filter((alert) => alert.status === "open").length,
      acknowledged: alerts.filter((alert) => alert.status === "acknowledged").length,
      resolved: alerts.filter((alert) => alert.status === "resolved").length,
    }),
    [alerts],
  );
  const activeCount = alerts.filter((alert) => alert.status !== "resolved").length;
  const progressScore = alerts.length ? Math.round(((counts.resolved + counts.acknowledged * 0.65) / alerts.length) * 100) : 100;
  const newestAlert = alerts.reduce<Alert | undefined>(
    (latest, alert) => (!latest || alert.detected_at > latest.detected_at ? alert : latest),
    undefined,
  );
  const topService =
    alerts
      .filter((alert) => alert.status !== "resolved")
      .reduce<Record<string, number>>((acc, alert) => {
        acc[alert.service] = (acc[alert.service] ?? 0) + 1;
        return acc;
      }, {});
  const busiestService =
    Object.entries(topService).sort((a, b) => b[1] - a[1])[0]?.[0] ?? "No active service";

  return (
    <section className="view">
      {err && <div className="error-banner">{err}</div>}

      <div className="metrics-grid">
        <MetricCard label="Critical active" value={counts.critical} tone="critical" icon="alert" detail="Needs review" />
        <MetricCard label="Open alerts" value={counts.open} tone={counts.open ? "warning" : "good"} icon="activity" detail="Untriaged drift" />
        <MetricCard label="Acknowledged" value={counts.acknowledged} icon="archive" detail="Owned by team" />
        <MetricCard label="Resolved" value={counts.resolved} tone="good" icon="check" detail="Closed loop" />
      </div>

      <div className="overview-grid">
        <InsightPanel
          eyebrow="Review posture"
          title={progressScore >= 80 ? "Queue is under control" : "Triage queue needs focus"}
          value={`${progressScore}%`}
          tone={progressScore >= 80 ? "good" : counts.critical ? "critical" : "warning"}
        >
          {alerts.length === 0
            ? "No dependency drift has been reported by the live collector."
            : `${counts.resolved + counts.acknowledged} of ${alerts.length} alerts have an owner or resolution.`}
        </InsightPanel>

        <section className="distribution-panel">
          <div className="panel-kicker">
            <span>Active severity</span>
            <b>{activeCount} active</b>
          </div>
          <StatusMeter label="Critical" value={counts.critical} total={activeCount} tone="critical" />
          <StatusMeter label="Warning" value={counts.warning} total={activeCount} tone="warning" />
          <StatusMeter label="Info" value={counts.info} total={activeCount} tone="accent" />
        </section>

        <InsightPanel
          eyebrow="Runtime signal"
          title={newestAlert ? `Latest drift in ${newestAlert.service}` : "No drift detected"}
          value={newestAlert ? relTime(newestAlert.detected_at) : "Live"}
          tone={counts.critical ? "critical" : counts.open ? "warning" : "accent"}
        >
          {activeCount > 0
            ? `${busiestService} has the highest active alert concentration.`
            : "SSE monitoring is connected and the review queue is clear."}
        </InsightPanel>
      </div>

      <div className="toolbar">
        <div>
          <h2>Alert review</h2>
          <p>Prioritize dependency updates that changed runtime behavior.</p>
        </div>
        <div className="segmented" role="tablist" aria-label="Alert status">
          {(["open", "acknowledged", "resolved", "all"] as const).map((item) => (
            <button key={item} className={status === item ? "active" : ""} onClick={() => setStatus(item)}>
              {STATUS_LABELS[item]}
            </button>
          ))}
        </div>
      </div>

      <div className="alert-list">
        {visible.length === 0 ? (
          <EmptyState icon="shield" title="No matching alerts" body="Dependencies are behaving within the selected review state." />
        ) : (
          visible.map((alert) => <AlertCard key={alert.id} alert={alert} onChange={load} />)
        )}
      </div>
    </section>
  );
}

function FingerprintCard({ fp }: { fp: Fingerprint }) {
  const entries = Object.entries(fp.behaviors).sort((a, b) => b[1].count - a[1].count);
  const top = entries.slice(0, 7);
  const max = Math.max(1, ...entries.map(([, stat]) => stat.count));

  return (
    <article className="fingerprint-card">
      <div className="fingerprint-head">
        <div>
          <div className="package-line">
            <span>{fp.package}</span>
            {fp.version && <span className="version-shift">@{fp.version}</span>}
          </div>
          <div className="alert-subtitle">
            <span>{fp.service}</span>
            <span>{fp.obs_count} observations</span>
            <span>{entries.length} behaviors</span>
          </div>
        </div>
        <span className={`state-chip ${fp.is_baseline ? "baseline" : "learning"}`}>
          {fp.is_baseline ? "Baseline" : "Learning"}
        </span>
      </div>

      <div className="behavior-table">
        {top.map(([name, stat]) => (
          <div className="behavior-row" key={name}>
            <span className="behavior-name">{name}</span>
            <span className="mini-bar" aria-hidden="true">
              <i style={{ width: `${Math.max(6, (stat.count / max) * 100)}%` }} />
            </span>
            <span className="behavior-count">x{stat.count}</span>
          </div>
        ))}
      </div>
    </article>
  );
}

function FingerprintsView() {
  const [fingerprints, setFingerprints] = useState<Fingerprint[]>([]);
  const [query, setQuery] = useState("");
  const getInitialMode = (): "all" | "baseline" | "learning" => {
    const value = new URLSearchParams(window.location.search).get("state");
    return value === "baseline" || value === "learning" ? value : "all";
  };
  const [mode, setMode] = useState<"all" | "baseline" | "learning">(getInitialMode);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    fetchFingerprints()
      .then((items) => {
        setFingerprints(items);
        setErr("");
      })
      .catch((e) => setErr(String(e)));
  }, []);

  useEffect(() => {
    load();
    if (window.location.search.includes("static")) {
      return;
    }
    const unsub = subscribe({ onEvents: () => load() });
    const timer = setInterval(load, 8000);
    return () => {
      unsub();
      clearInterval(timer);
    };
  }, [load]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return fingerprints
      .filter((fp) => (mode === "all" ? true : mode === "baseline" ? fp.is_baseline : !fp.is_baseline))
      .filter((fp) => !needle || fp.package.toLowerCase().includes(needle) || fp.service.toLowerCase().includes(needle))
      .sort((a, b) => Number(b.is_baseline) - Number(a.is_baseline) || b.obs_count - a.obs_count);
  }, [fingerprints, mode, query]);

  const behaviorCount = fingerprints.reduce((sum, fp) => sum + Object.keys(fp.behaviors).length, 0);
  const baselineCount = fingerprints.filter((fp) => fp.is_baseline).length;
  const learningCount = fingerprints.length - baselineCount;
  const baselineCoverage = pct(baselineCount, fingerprints.length);
  const observationCount = fingerprints.reduce((sum, fp) => sum + fp.obs_count, 0);
  const latestFingerprint = fingerprints.reduce<Fingerprint | undefined>(
    (latest, fp) => (!latest || fp.last_seen > latest.last_seen ? fp : latest),
    undefined,
  );

  return (
    <section className="view">
      {err && <div className="error-banner">{err}</div>}

      <div className="metrics-grid">
        <MetricCard label="Packages learned" value={fingerprints.length} icon="cube" detail="Across services" />
        <MetricCard label="Baselines" value={baselineCount} tone="good" icon="shield" detail="Promoted" />
        <MetricCard label="Learning" value={learningCount} tone="warning" icon="activity" detail="Still observing" />
        <MetricCard label="Behaviors" value={behaviorCount} tone="accent" icon="grid" detail="Canonical signals" />
      </div>

      <div className="overview-grid fingerprint-overview">
        <InsightPanel
          eyebrow="Library quality"
          title={baselineCoverage >= 70 ? "Baseline coverage is strong" : "Promote stable fingerprints"}
          value={`${baselineCoverage}%`}
          tone={baselineCoverage >= 70 ? "good" : "warning"}
        >
          {fingerprints.length === 0
            ? "Run monitored workloads to populate package behavior fingerprints."
            : `${baselineCount} of ${fingerprints.length} fingerprints are promoted baselines.`}
        </InsightPanel>

        <section className="distribution-panel">
          <div className="panel-kicker">
            <span>Fingerprint state</span>
            <b>{filtered.length} visible</b>
          </div>
          <StatusMeter label="Baseline" value={baselineCount} total={fingerprints.length} tone="good" />
          <StatusMeter label="Learning" value={learningCount} total={fingerprints.length} tone="warning" />
          <StatusMeter label="Filtered" value={filtered.length} total={fingerprints.length} tone="accent" />
        </section>

        <InsightPanel
          eyebrow="Signal volume"
          title={latestFingerprint ? `Freshest package: ${latestFingerprint.package}` : "Awaiting observations"}
          value={observationCount.toLocaleString()}
          tone={observationCount > 0 ? "accent" : "neutral"}
        >
          {latestFingerprint
            ? `${latestFingerprint.service} updated ${relTime(latestFingerprint.last_seen)} with ${Object.keys(latestFingerprint.behaviors).length} behaviors.`
            : "Collector observations will appear here as eBPF events are attributed."}
        </InsightPanel>
      </div>

      <div className="toolbar">
        <div>
          <h2>Fingerprint explorer</h2>
          <p>Inspect learned package behavior before and after dependency updates.</p>
        </div>
        <label className="search-box">
          <Icon name="search" />
          <input placeholder="Search package or service" value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
      </div>

      <div className="subtoolbar">
        <div className="segmented compact" role="tablist" aria-label="Fingerprint state">
          {(["all", "baseline", "learning"] as const).map((item) => (
            <button key={item} className={mode === item ? "active" : ""} onClick={() => setMode(item)}>
              {item[0].toUpperCase() + item.slice(1)}
            </button>
          ))}
        </div>
        <span>{filtered.length} results</span>
      </div>

      <div className="fingerprint-list">
        {filtered.length === 0 ? (
          <EmptyState icon="fingerprint" title="No fingerprints found" body="Run a watched workload or adjust the current filters." />
        ) : (
          filtered.map((fp) => <FingerprintCard key={`${fp.service}/${fp.package}/${fp.version}`} fp={fp} />)
        )}
      </div>
    </section>
  );
}

// TokenGate blocks the workspace when the collector requires an API token
// (GOODMAN_API_TOKEN) and none, or a wrong one, is stored locally.
function TokenGate() {
  const [value, setValue] = useState("");
  const hadToken = getToken() !== "";

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const token = value.trim();
    if (!token) return;
    setToken(token);
    window.location.reload();
  };

  return (
    <div className="token-gate">
      <form className="token-card" onSubmit={submit}>
        <div className="token-head">
          <Icon name="lock" />
          <h2>API token required</h2>
        </div>
        <p>
          {hadToken
            ? "The stored token was rejected by the collector. Enter the current API token to continue."
            : "This collector requires an API token (GOODMAN_API_TOKEN). Ask your operator or read it from the goodman Kubernetes secret."}
        </p>
        <input
          type="password"
          autoFocus
          placeholder="Paste API token"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          aria-label="API token"
        />
        <button type="submit" className="primary-action" disabled={!value.trim()}>
          <Icon name="check" />
          Unlock dashboard
        </button>
      </form>
    </div>
  );
}

export function App() {
  const getHashTab = (): Tab => {
    const h = window.location.hash.replace("#", "");
    return h === "fingerprints" ? "fingerprints" : "alerts";
  };
  const [tab, setTab] = useState<Tab>(getHashTab);
  const [openCount, setOpenCount] = useState(0);
  const [knownPackages, setKnownPackages] = useState(0);
  const [lastRefresh, setLastRefresh] = useState<Date>(() => new Date());
  const [authRequired, setAuthRequired] = useState(false);

  useEffect(() => {
    onUnauthorized(() => setAuthRequired(true));
  }, []);

  useEffect(() => {
    const handleHashChange = () => {
      setTab(getHashTab());
    };
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, []);

  useEffect(() => {
    const refresh = () => {
      Promise.allSettled([fetchAlerts("open"), fetchFingerprints()]).then(([alertsResult, fingerprintsResult]) => {
        if (alertsResult.status === "fulfilled") setOpenCount(alertsResult.value.length);
        if (fingerprintsResult.status === "fulfilled") setKnownPackages(fingerprintsResult.value.length);
        setLastRefresh(new Date());
      });
    };
    refresh();
    if (window.location.search.includes("static")) {
      return;
    }
    const unsub = subscribe({ onAlerts: refresh, onEvents: refresh });
    const timer = setInterval(refresh, 5000);
    return () => {
      unsub();
      clearInterval(timer);
    };
  }, []);

  if (authRequired) {
    return <TokenGate />;
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="logo-mark">
            <Icon name="triangle" />
          </div>
          <div>
            <h1>GOODMAN</h1>
            <p>by Heisenbug</p>
          </div>
        </div>

        <nav className="nav-list" aria-label="Dashboard sections">
          <button className={tab === "alerts" ? "active" : ""} onClick={() => setTab("alerts")}>
            <Icon name="alert" />
            <span>Alerts</span>
            {openCount > 0 && <b>{openCount}</b>}
          </button>
          <button className={tab === "fingerprints" ? "active" : ""} onClick={() => setTab("fingerprints")}>
            <Icon name="fingerprint" />
            <span>Fingerprints</span>
          </button>
        </nav>

        <div className="side-panel">
          <div className="side-panel-head">
            <Icon name="spark" />
            <span>Live sensor</span>
          </div>
          <strong>{openCount === 0 ? "Healthy" : `${openCount} open`}</strong>
          <p>{knownPackages} learned package fingerprints</p>
          <div className="side-meter" aria-hidden="true">
            <i style={{ width: `${Math.min(100, knownPackages * 8)}%` }} />
          </div>
        </div>
      </aside>

      <main className="workspace">
        <header className="workspace-head">
          <div>
            <p className="eyebrow">Runtime security index</p>
            <h2>{tab === "alerts" ? "Dependency drift review" : "Behavior fingerprint library"}</h2>
          </div>
          <div className="monitor-pill">
            <i />
            <span>Monitoring</span>
            <small>{lastRefresh.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</small>
          </div>
        </header>

        {tab === "alerts" ? <AlertsView /> : <FingerprintsView />}
      </main>
    </div>
  );
}
