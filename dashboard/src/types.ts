export type Severity = "INFO" | "WARN" | "CRITICAL";
export type AlertStatus = "open" | "acknowledged" | "resolved";

export interface Alert {
  id: string;
  service: string;
  package: string;
  old_version: string;
  new_version: string;
  severity: Severity;
  baseline_behaviors?: string[];
  new_behaviors: string[];
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
}

export interface AttributedEvent {
  service: string;
  package: string;
  version: string;
  type: number;
  behavior: string;
  timestamp: number;
}
