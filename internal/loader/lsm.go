// Optional fail-open LSM load and attach lifecycle.
package loader

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/features"
	"github.com/cilium/ebpf/link"
)

func deleteEnforcementPrograms(spec *ebpf.CollectionSpec) {
	delete(spec.Programs, "enforce_file_open")
	delete(spec.Programs, "enforce_socket_connect")
	delete(spec.Programs, "enforce_bprm_check")
}

func keepEnforcementPrograms(spec *ebpf.CollectionSpec) {
	for name := range spec.Programs {
		switch name {
		case "enforce_file_open", "enforce_socket_connect", "enforce_bprm_check":
		default:
			delete(spec.Programs, name)
		}
	}
}

func (l *Loader) tryAttachLSM(baseSpec *ebpf.CollectionSpec) {
	reason := lsmSupportReason()
	if reason != "" {
		l.enforceReason = reason
		log.Printf("loader: LSM enforcement unavailable (%s); detection-only", reason)
		return
	}

	enforcementSpec := baseSpec.Copy()
	keepEnforcementPrograms(enforcementSpec)
	coll, err := ebpf.NewCollectionWithOptions(enforcementSpec, ebpf.CollectionOptions{
		MapReplacements: l.coll.Maps,
	})
	if err != nil {
		l.enforceReason = fmt.Sprintf("load LSM programs: %v", err)
		log.Printf("loader: LSM enforcement unavailable (%s); detection-only", l.enforceReason)
		return
	}
	l.enforceColl = coll
	var lsmLinks []link.Link
	for _, name := range []string{"enforce_file_open", "enforce_socket_connect", "enforce_bprm_check"} {
		prog := coll.Programs[name]
		if prog == nil {
			l.enforceReason = "LSM program missing from collection"
			coll.Close()
			l.enforceColl = nil
			return
		}
		lnk, err := link.AttachLSM(link.LSMOptions{Program: prog})
		if err != nil {
			l.enforceReason = fmt.Sprintf("attach %s: %v", name, err)
			for _, existing := range lsmLinks {
				if existing != nil {
					_ = existing.Close()
				}
			}
			coll.Close()
			l.enforceColl = nil
			log.Printf("loader: LSM attach failed (%s); detection-only", l.enforceReason)
			return
		}
		lsmLinks = append(lsmLinks, lnk)
	}
	l.links = append(l.links, lsmLinks...)
	l.enforceActive = true
	l.enforceReason = "active"
	log.Printf("loader: LSM enforcement programs attached")
}

func lsmSupportReason() string {
	if err := features.HaveProgramType(ebpf.LSM); err != nil {
		return err.Error()
	}
	b, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil {
		return "cannot read /sys/kernel/security/lsm"
	}
	if !strings.Contains(string(b), "bpf") {
		return `active lsm= list does not include "bpf"`
	}
	return ""
}

func (l *Loader) EnforcementActive() bool { return l.enforceActive }

func (l *Loader) EnforcementReason() string {
	if l.enforceReason == "" {
		if l.enforceRequested {
			return "not probed"
		}
		return "disabled"
	}
	return l.enforceReason
}
