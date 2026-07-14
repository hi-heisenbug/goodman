package enforce

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/store"
)

const maxTrackedBehaviors = 1024

// Manager holds runtime enforcement state and compiled verdicts.
type Manager struct {
	mu           sync.RWMutex
	masterGate   bool
	enabled      bool
	rev          int
	verdicts     VerdictSet
	behaviors    map[string]bool
	rules        []diff.Rule
	sensorHB     map[string]time.Time
	sensorActive map[string]bool

	store *store.Store
}

func NewManager(st *store.Store, masterGate bool) *Manager {
	m := &Manager{
		masterGate:   masterGate,
		behaviors:    map[string]bool{},
		sensorHB:     map[string]time.Time{},
		sensorActive: map[string]bool{},
		store:        st,
	}
	if st != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		en, rev, _ := st.GetEnforceState(ctx)
		m.enabled = en && masterGate
		m.rev = rev
	}
	return m
}

func (m *Manager) SetRules(rules []diff.Rule) {
	m.mu.Lock()
	m.rules = rules
	rev := m.recomputeLocked()
	m.mu.Unlock()
	m.persistRev(rev)
}

func (m *Manager) MasterGate() bool { return m.masterGate }

func (m *Manager) RecordBehavior(behavior string) {
	if behavior == "" {
		return
	}
	m.mu.Lock()
	if m.behaviors[behavior] || len(m.behaviors) >= maxTrackedBehaviors || !m.matchesBlockRuleLocked(behavior) {
		m.mu.Unlock()
		return
	}
	m.behaviors[behavior] = true
	m.verdicts = mergeVerdictSets(m.verdicts, CompileVerdicts(m.rules, []string{behavior}))
	m.rev++
	rev := m.rev
	m.mu.Unlock()
	m.persistRev(rev)
}

func mergeVerdictSets(current, added VerdictSet) VerdictSet {
	open := make(map[string]bool, len(current.Open))
	for _, path := range current.Open {
		open[path] = true
	}
	for _, path := range added.Open {
		if !open[path] {
			open[path] = true
			current.Open = append(current.Open, path)
		}
	}
	connect := make(map[ConnectVerdict]bool, len(current.Connect))
	for _, addr := range current.Connect {
		connect[addr] = true
	}
	for _, addr := range added.Connect {
		if !connect[addr] {
			connect[addr] = true
			current.Connect = append(current.Connect, addr)
		}
	}
	exec := make(map[string]bool, len(current.Exec))
	for _, path := range current.Exec {
		exec[path] = true
	}
	for _, path := range added.Exec {
		if !exec[path] {
			exec[path] = true
			current.Exec = append(current.Exec, path)
		}
	}
	skipped := make(map[string]bool, len(current.Skipped))
	for _, verdict := range current.Skipped {
		skipped[verdict.Behavior] = true
	}
	for _, verdict := range added.Skipped {
		if !skipped[verdict.Behavior] {
			skipped[verdict.Behavior] = true
			current.Skipped = append(current.Skipped, verdict)
		}
	}
	return current
}

func (m *Manager) matchesBlockRuleLocked(behavior string) bool {
	for i := range m.rules {
		if m.rules[i].Action == diff.ActionBlock && m.rules[i].Matches(behavior) {
			return true
		}
	}
	return false
}

func (m *Manager) recomputeLocked() int {
	if !m.masterGate {
		m.verdicts = VerdictSet{}
		return m.rev
	}
	beh := make([]string, 0, len(m.behaviors))
	for b := range m.behaviors {
		beh = append(beh, b)
	}
	m.verdicts = CompileVerdicts(m.rules, beh)
	m.rev++
	return m.rev
}

func (m *Manager) persistRev(rev int) {
	if m.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.store.SetEnforceRev(ctx, rev)
	}
}

func (m *Manager) SetEnabled(ctx context.Context, on bool) error {
	if on && !m.masterGate {
		return ErrMasterGateOff
	}
	enabled := on && m.masterGate
	if m.store != nil {
		if err := m.store.SetEnforceEnabled(ctx, enabled); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.enabled = enabled
	rev := m.recomputeLocked()
	m.mu.Unlock()
	m.persistRev(rev)
	return nil
}

func (m *Manager) Enabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

func (m *Manager) Rev() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rev
}

func (m *Manager) StateForSensor() (enabled bool, rev int, vs VerdictSet) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled, m.rev, m.verdicts
}

func (m *Manager) Status() (enabled, master bool, rev int, vs VerdictSet, sensors map[string]SensorStatus) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sensors = make(map[string]SensorStatus, len(m.sensorHB))
	for name, t := range m.sensorHB {
		sensors[name] = SensorStatus{
			LastHeartbeat: t,
			Active:        m.sensorActive[name],
		}
	}
	return m.enabled, m.masterGate, m.rev, m.verdicts, sensors
}

type SensorStatus struct {
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Active        bool      `json:"enforcement_active"`
}

func (m *Manager) RecordSensorHeartbeat(sensor string, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sensorHB[sensor] = time.Now()
	m.sensorActive[sensor] = active
}

var ErrMasterGateOff = errors.New("collector not started with -enforce-enabled (master gate off)")
