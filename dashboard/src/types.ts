export type Severity = "INFO" | "WARN" | "CRITICAL";
export type AlertStatus = "open" | "acknowledged" | "resolved";

export interface AlertEvidence {
  behavior: string;
  rules?: string[];
  sensor?: string;
  first_seen?: number; // unix ns
}

export interface Alert {
  id: string;
  service: string;
  package: string;
  old_version: string;
  new_version: string;
  severity: Severity;
  baseline_behaviors?: string[];
  new_behaviors: string[];
  matched_rules?: string[];
  would_block?: boolean;
  evidence?: AlertEvidence[];
  detected_at: number; // unix ns
  status: AlertStatus;
}

export interface BehaviorStat {
  count: number;
  first: number;
  last: number;
}

export interface Fingerprint {
  service: string;
  package: string;
  version: string;
  behaviors: Record<string, BehaviorStat>;
  first_seen: number;
  last_seen: number;
  obs_count: number;
  is_baseline: boolean;
  origin?: "local" | "imported";
}

export interface AttributedEvent {
  service: string;
  package: string;
  version: string;
  type: number;
  behavior: string;
  timestamp: number;
}

export interface ReportVuln {
  id: string;
  summary?: string;
  severity: string;
}

export interface ReportRow {
  name: string;
  declared_version: string;
  dev?: boolean;
  executed: boolean;
  executed_version?: string;
  behaviors?: number;
  vulns?: ReportVuln[];
}

export interface Report {
  service?: string;
  declared_count: number;
  executed_count: number;
  vuln_rows: ReportRow[];
  rows: ReportRow[];
}

export interface ReportDelta {
  executed: number;
  declared: number;
  reachable_vulns: number;
  new_executed_packages?: string[];
  new_reachable_vuln_ids?: string[];
  previous_computed_at?: number;
}

export interface StoredReport {
  computed_at: number; // unix ns
  osv: boolean;
  report: Report;
  previous?: {
    computed_at: number;
    report: Report;
  };
  delta?: ReportDelta;
}

export interface CoverageSnapshot {
  sensors: Array<{
    name: string;
    status: string;
    last_seen: number;
    events_per_sec: number;
    events_total: number;
  }>;
  attribution: {
    package: number;
    app: number;
    unknown: number;
    success_rate: number;
    top_unknown: Array<{ service: string; count: number }>;
  };
  namespaces: Array<{
    name: string;
    inject_label: boolean;
    pods_total: number;
    pods_with_node_options: number;
    pods_without: number;
    reported_by?: string;
    reported_at?: number;
  }>;
  alert_budget: {
    target_per_day: number;
    alerts_last_24h: number;
    would_block_last_24h: number;
  };
}

