import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

type ObserveProof = {
  schema: string;
  source: string;
  command: string;
  package: string;
  version: string;
  behavior: string;
  events: number;
  unique_behaviors: number;
  exact_dependency_events: number;
  pass: string;
};

const proof = JSON.parse(
  readFileSync(resolve(__dirname, "..", "evidence", "observe_proof.json"), "utf8"),
) as ObserveProof;

describe("real-workload observe proof", () => {
  it("comes from the verified host-kernel path", () => {
    expect(proof.schema).toBe("goodman.demo.observe-proof/v1");
    expect(proof.source).toBe("live host process via privileged Docker and host kernel");
    expect(proof.command).toContain("scripts/setup-everything.sh observe");
    expect(proof.command).toContain("--live-backend docker");
  });

  it("proves one exact versioned dependency without leaking a local path", () => {
    expect(proof.package).toBe("good-pkg");
    expect(proof.version).toBe("1.0.0");
    expect(proof.behavior).toBe("READ …/node_modules/good-pkg/**");
    expect(proof.behavior).not.toContain("/home/");
    expect(proof.events).toBeGreaterThan(0);
    expect(proof.unique_behaviors).toBeGreaterThan(0);
    expect(proof.exact_dependency_events).toBe(proof.events);
    expect(proof.pass).toBe(
      "PASS: Goodman attributed real syscalls to 1 dependency identity.",
    );
  });
});
