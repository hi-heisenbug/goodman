// One-shot live attribution command.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/hi-heisenbug/goodman/internal/attribute"
	"github.com/hi-heisenbug/goodman/internal/loader"
)

// cmdAttribute loads the eBPF sensor for a single pid and prints attributed
// events live — the §5 DoD check. Needs root.
func cmdAttribute(args []string) {
	fs := flag.NewFlagSet("attribute", flag.ExitOnError)
	pid := fs.Int("pid", 0, "target pid (required)")
	dur := fs.Duration("duration", 15*time.Second, "how long to trace")
	procRoot := fs.String("proc-root", "/proc", "proc mount")
	showStacks := fs.Bool("stacks", false, "print resolved stack frames per event")
	fs.Parse(args)
	if *pid <= 0 {
		log.Fatal("usage: goodmanctl attribute -pid <pid> [-duration 15s] [-stacks]")
	}

	l, err := loader.New(*procRoot)
	if err != nil {
		log.Fatalf("load eBPF (need root): %v", err)
	}
	defer l.Close()
	if err := l.Watch(uint32(*pid)); err != nil {
		log.Fatalf("watch pid %d: %v", *pid, err)
	}
	resolver := attribute.NewResolver(*procRoot)
	bootOffset := loader.BootToUnixNs()
	log.Printf("tracing pid %d for %s…", *pid, *dur)

	go func() {
		time.Sleep(*dur)
		l.Close()
	}()
	count := map[string]int{}
	for {
		ev, err := l.Read()
		if err != nil {
			break
		}
		at := resolver.Attribute(ev, bootOffset)
		fmt.Printf("%s | %s@%s | %s\n", at.Service, at.Package, orDash(at.Version), at.Behavior)
		count[at.Package]++
		if *showStacks {
			for _, f := range resolver.ResolveStack(*pid, ev.UserStack()) {
				loc := f.Symbol
				if f.Source != "" {
					loc = f.Source
				}
				fmt.Printf("      %#014x  %s\n", f.Addr, loc)
			}
		}
	}
	fmt.Println("\nattribution summary:")
	for pkg, n := range count {
		fmt.Printf("  %-40s %d\n", pkg, n)
	}
}
