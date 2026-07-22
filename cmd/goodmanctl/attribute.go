// One-shot live attribution command.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/loader"
)

// cmdAttribute loads the eBPF sensor for a single pid and prints attributed
// events live — the §5 DoD check. Needs root.
func cmdAttribute(args []string) {
	fs := flag.NewFlagSet("attribute", flag.ExitOnError)
	pid := fs.Int("pid", 0, "target pid (auto-select when exactly one supported runtime is running)")
	dur := fs.Duration("duration", 15*time.Second, "how long to trace")
	procRoot := fs.String("proc-root", "/proc", "proc mount")
	showStacks := fs.Bool("stacks", false, "print resolved stack frames per event")
	dedupe := fs.Bool("dedupe", false, "print each package behavior only once")
	verify := fs.Bool("verify", false, "fail unless at least one exact dependency is attributed")
	fs.Parse(args)
	if *dur <= 0 {
		log.Fatal("duration must be greater than zero")
	}
	target, err := resolveAttributeTarget(*procRoot, *pid)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("target pid=%d comm=%s command=%s", target.PID, target.Comm, target.Command)
	ready, readiness := attributeTargetReadiness(*procRoot, target)
	if ready {
		log.Print(readiness)
	} else {
		log.Printf("WARNING: %s", readiness)
	}

	l, err := loader.New(*procRoot)
	if err != nil {
		log.Fatalf("load eBPF (need root): %v", err)
	}
	defer l.Close()
	if err := l.Watch(uint32(target.PID)); err != nil {
		log.Fatalf("watch pid %d: %v", target.PID, err)
	}
	resolver := attribute.NewResolver(*procRoot)
	bootOffset := loader.BootToUnixNs()
	log.Printf("tracing pid %d for %s; generate real workload traffic now…", target.PID, *dur)

	go func() {
		time.Sleep(*dur)
		l.Close()
	}()
	proof := newAttributeProof()
	for {
		ev, err := l.Read()
		if err != nil {
			break
		}
		at := resolver.Attribute(ev, bootOffset)
		unique := proof.Record(at)
		if !*dedupe || unique {
			fmt.Printf("%s | %s@%s | %s\n", at.Service, at.Package, orDash(at.Version), at.Behavior)
		}
		if *showStacks {
			for _, f := range resolver.ResolveStack(target.PID, ev.UserStack()) {
				loc := f.Symbol
				if f.Source != "" {
					loc = f.Source
				}
				fmt.Printf("      %#014x  %s\n", f.Addr, loc)
			}
		}
	}
	proof.WriteSummary(os.Stdout)
	if *verify {
		if err := proof.Verify(); err != nil {
			log.Fatalf("verification failed: %v", err)
		}
		fmt.Println(proof.PassMessage())
	}
}
