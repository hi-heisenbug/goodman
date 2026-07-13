package enforce

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hi-heisenbug/goodman/internal/diff"
	"github.com/hi-heisenbug/goodman/internal/store"
)

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
		ctx := context.Background()
		en, rev, _ := st.GetEnforceState(ctx)
		m.enabled = en && masterGate
		m.rev = rev
	}
	return m
}

func (m *Manager) SetRules(rules []diff.Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = rules
	m.recomputeLocked()
}

func (m *Manager) MasterGate() bool { return m.masterGate }

func (m *Manager) RecordBehavior(behavior string) {
	if behavior == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.behaviors[behavior] {
		return
	}
	m.behaviors[behavior] = true
	m.recomputeLocked()
}

func (m *Manager) recomputeLocked() {
	if !m.masterGate {
		m.verdicts = VerdictSet{}
		return
	}
	beh := make([]string, 0, len(m.behaviors))
	for b := range m.behaviors {
		beh = append(beh, b)
	}
	m.verdicts = CompileVerdicts(m.rules, beh)
	m.rev++
	if m.store != nil {
		_ = m.store.SetEnforceRev(context.Background(), m.rev)
	}
}

func (m *Manager) SetEnabled(ctx context.Context, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on && !m.masterGate {
		return ErrMasterGateOff
	}
	m.enabled = on && m.masterGate
	if m.store != nil {
		if err := m.store.SetEnforceEnabled(ctx, m.enabled); err != nil {
			return err
		}
	}
	m.recomputeLocked()
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
