package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/hi-heisenbug/goodman/internal/model"
)

type attributeProof struct {
	events          int
	exactEvents     int
	behaviors       map[string]struct{}
	identities      map[string]int
	exactIdentities map[string]struct{}
}

func newAttributeProof() *attributeProof {
	return &attributeProof{
		behaviors:       make(map[string]struct{}),
		identities:      make(map[string]int),
		exactIdentities: make(map[string]struct{}),
	}
}

func (p *attributeProof) Record(event model.Attributed) bool {
	identity := attributedIdentity(event)
	behavior := identity + "\x00" + event.Behavior
	_, seen := p.behaviors[behavior]
	p.behaviors[behavior] = struct{}{}
	p.identities[identity]++
	p.events++
	if isExactDependency(event) {
		p.exactEvents++
		p.exactIdentities[identity] = struct{}{}
	}
	return !seen
}

func (p *attributeProof) WriteSummary(w io.Writer) {
	fmt.Fprintf(w, "\nproof summary: %d events, %d unique behaviors, %d exact dependency events\n",
		p.events, len(p.behaviors), p.exactEvents)
	identities := make([]string, 0, len(p.identities))
	for identity := range p.identities {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		fmt.Fprintf(w, "  %-44s %d\n", identity, p.identities[identity])
	}
}

func (p *attributeProof) Verify() error {
	if p.events == 0 {
		return fmt.Errorf("no syscalls were captured; generate traffic against the target during the trace and retry")
	}
	if p.exactEvents == 0 {
		return fmt.Errorf("syscalls were captured, but no dependency with a version was attributed; apply the runtime profiling hint above and retry")
	}
	return nil
}

func (p *attributeProof) ExactIdentityCount() int {
	return len(p.exactIdentities)
}

func (p *attributeProof) PassMessage() string {
	count := p.ExactIdentityCount()
	identity := "dependency identities"
	if count == 1 {
		identity = "dependency identity"
	}
	return fmt.Sprintf("PASS: Goodman attributed real syscalls to %d %s.", count, identity)
}

func attributedIdentity(event model.Attributed) string {
	name := event.Package
	if name == "" {
		name = "<unknown>"
	}
	return name + "@" + orDash(event.Version)
}

func isExactDependency(event model.Attributed) bool {
	return event.Package != "" && event.Package != "<app>" && event.Package != "<unknown>" && event.Version != ""
}
