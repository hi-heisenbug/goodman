package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
)

func (s *Store) PruneResolvedAlerts(ctx context.Context, cutoff time.Time) (int64, error) {
	cutoffNs := cutoff.UnixNano()
	if cutoffNs < 0 {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM alerts WHERE status=$1 AND detected_at < $2`,
		model.AlertResolved, uint64(cutoffNs))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) UpsertAlert(ctx context.Context, alert *model.Alert) (created bool, err error) {
	const maxAttempts = 4
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var retry bool
		created, retry, err = s.upsertAlertTx(ctx, alert)
		if err != nil || !retry {
			return created, err
		}
	}
	created, _, err = s.upsertAlertTx(ctx, alert)
	return created, err
}

func (s *Store) upsertAlertTx(ctx context.Context, alert *model.Alert) (created bool, retry bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback() //nolint:errcheck
	existing, err := s.getAlertForUpdate(ctx, tx, alert.ID)
	if err != nil {
		return false, false, err
	}
	if existing != nil {
		mergeIntoAlert(existing, alert)
		if err := s.updateAlertRow(ctx, tx, alert); err != nil {
			return false, false, err
		}
		return false, false, tx.Commit()
	}
	if err := s.insertAlertRow(ctx, tx, alert); err != nil {
		if isUniqueViolation(err) {
			return false, true, nil
		}
		return false, false, err
	}
	return true, false, tx.Commit()
}

func (s *Store) getAlertForUpdate(ctx context.Context, tx *sql.Tx, id string) (*model.Alert, error) {
	query := `SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, blocked, detected_at, status
	          FROM alerts WHERE id=$1`
	if s.dialect == "postgres" {
		query += ` FOR UPDATE`
	}
	return scanAlert(tx.QueryRowContext(ctx, query, id))
}

func mergeIntoAlert(existing, incoming *model.Alert) {
	incoming.WouldBlock = existing.WouldBlock || incoming.WouldBlock
	incoming.Blocked = existing.Blocked || incoming.Blocked
	incoming.MatchedRules = mergeStrings(existing.MatchedRules, incoming.MatchedRules)
	incoming.NewBehaviors = mergeStrings(existing.NewBehaviors, incoming.NewBehaviors)
	incoming.Evidence = mergeEvidence(existing.Evidence, incoming.Evidence)
	incoming.Severity = maxSeverity(existing.Severity, incoming.Severity)
	if existing.Status == model.AlertResolved {
		incoming.Status = model.AlertOpen
		return
	}
	incoming.Status = existing.Status
	incoming.DetectedAt = existing.DetectedAt
}

func (s *Store) updateAlertRow(ctx context.Context, tx *sql.Tx, alert *model.Alert) error {
	newBehaviors, rules, evidence := marshalAlertCollections(alert)
	_, err := tx.ExecContext(ctx,
		`UPDATE alerts SET new_behaviors=$1, severity=$2, matched_rules=$3, evidence=$4, would_block=$5, blocked=$6, status=$7, detected_at=$8 WHERE id=$9`,
		newBehaviors, alert.Severity, rules, evidence, alert.WouldBlock,
		alert.Blocked, alert.Status, alert.DetectedAt, alert.ID)
	return err
}

func (s *Store) insertAlertRow(ctx context.Context, tx *sql.Tx, alert *model.Alert) error {
	newBehaviors, rules, evidence := marshalAlertCollections(alert)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO alerts (id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, blocked, detected_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		alert.ID, alert.Service, alert.Package, alert.OldVersion, alert.NewVersion,
		alert.Severity, newBehaviors, rules, evidence, alert.WouldBlock,
		alert.Blocked, alert.DetectedAt, alert.Status)
	return err
}

func marshalAlertCollections(alert *model.Alert) (newBehaviors, rules, evidence string) {
	newBehaviorsJSON, _ := json.Marshal(alert.NewBehaviors)
	rulesJSON, _ := json.Marshal(nonNilStrings(alert.MatchedRules))
	evidenceJSON, _ := json.Marshal(nonNilEvidence(alert.Evidence))
	return string(newBehaviorsJSON), string(rulesJSON), string(evidenceJSON)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "23505")
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilEvidence(values []model.Evidence) []model.Evidence {
	if values == nil {
		return []model.Evidence{}
	}
	return values
}

func mergeEvidence(first, second []model.Evidence) []model.Evidence {
	byBehavior := map[string]int{}
	out := make([]model.Evidence, 0, len(first)+len(second))
	for _, evidence := range append(append([]model.Evidence{}, first...), second...) {
		index, seen := byBehavior[evidence.Behavior]
		if !seen {
			byBehavior[evidence.Behavior] = len(out)
			out = append(out, evidence)
			continue
		}
		if evidence.FirstSeen != 0 && (out[index].FirstSeen == 0 || evidence.FirstSeen < out[index].FirstSeen) {
			out[index].FirstSeen = evidence.FirstSeen
			out[index].Sensor = evidence.Sensor
		}
		out[index].Rules = mergeStrings(out[index].Rules, evidence.Rules)
	}
	return out
}

func (s *Store) GetAlert(ctx context.Context, id string) (*model.Alert, error) {
	return scanAlert(s.db.QueryRowContext(ctx,
		`SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, blocked, detected_at, status
		 FROM alerts WHERE id=$1`, id))
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAlert(row rowScanner) (*model.Alert, error) {
	var alert model.Alert
	var newBehaviorsJSON, rulesJSON, evidenceJSON []byte
	var oldVersion sql.NullString
	err := row.Scan(&alert.ID, &alert.Service, &alert.Package, &oldVersion,
		&alert.NewVersion, &alert.Severity, &newBehaviorsJSON, &rulesJSON,
		&evidenceJSON, &alert.WouldBlock, &alert.Blocked, &alert.DetectedAt, &alert.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	alert.OldVersion = oldVersion.String
	if err := json.Unmarshal(newBehaviorsJSON, &alert.NewBehaviors); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rulesJSON, &alert.MatchedRules); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(evidenceJSON, &alert.Evidence); err != nil {
		return nil, err
	}
	return &alert, nil
}

func (s *Store) ListAlerts(ctx context.Context, status string) ([]model.Alert, error) {
	return s.ListAlertsPage(ctx, status, 500, 0)
}

func (s *Store) ListAlertsPage(ctx context.Context, status string, limit, offset int) ([]model.Alert, error) {
	limit, offset = normalizePage(limit, offset)
	query := `SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, blocked, detected_at, status FROM alerts`
	var args []any
	if status != "" {
		query += " WHERE status=$1"
		args = append(args, status)
	}
	query += fmt.Sprintf(" ORDER BY detected_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Alert
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *alert)
	}
	return out, rows.Err()
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Store) FindUnblockedAlertByBehavior(ctx context.Context, service, pkg, behavior string) (*model.Alert, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, service, package, old_version, new_version, severity, new_behaviors, matched_rules, evidence, would_block, blocked, detected_at, status
		FROM alerts WHERE service=$1 AND package=$2 AND blocked=FALSE
		ORDER BY detected_at DESC`, service, pkg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		for _, candidate := range alert.NewBehaviors {
			if candidate == behavior {
				return alert, nil
			}
		}
	}
	return nil, rows.Err()
}

func (s *Store) CountAlertsSince(ctx context.Context, sinceNs uint64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alerts WHERE detected_at >= $1`, sinceNs).Scan(&count)
	return count, err
}

func (s *Store) CountWouldBlockSince(ctx context.Context, sinceNs uint64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alerts WHERE would_block AND detected_at >= $1`, sinceNs).Scan(&count)
	return count, err
}

func (s *Store) SetAlertStatus(ctx context.Context, id, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE alerts SET status=$1 WHERE id=$2`, status, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func mergeStrings(first, second []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range append(append([]string{}, first...), second...) {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

var severityRank = map[string]int{
	model.SeverityInfo:     0,
	model.SeverityWarn:     1,
	model.SeverityCritical: 2,
}

func maxSeverity(first, second string) string {
	if severityRank[second] > severityRank[first] {
		return second
	}
	return first
}

func (s *Store) SetAlertBlocked(ctx context.Context, id string, blocked bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET blocked=$1 WHERE id=$2`, blocked, id)
	return err
}
