import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import plan from "../interaction_plan.json";

describe("interactive Goodman walkthrough", () => {
  it("covers alert arrival, fingerprints, reachability, and coverage", () => {
    expect(Object.keys(plan.segments)).toEqual([
      "alerts",
      "fingerprints",
      "reachability",
      "coverage",
    ]);

    expect(Object.values(plan.segments)[0].start).toBe(0);
    let previousEnd = 0;
    for (const segment of Object.values(plan.segments)) {
      expect(segment.start).toBe(previousEnd);
      expect(segment.end).toBeGreaterThan(segment.start);
      previousEnd = segment.end;
    }
    expect(previousEnd).toBe(plan.duration_seconds);
  });

  it("keeps every cursor action ordered and inside the recording", () => {
    const actionTimes = plan.actions.map((action) => action.at);
    const labels = plan.actions.map((action) => action.label);
    expect(actionTimes).toEqual([...actionTimes].sort((left, right) => left - right));
    expect(new Set(labels).size).toBe(labels.length);
    for (const action of plan.actions) {
      expect(action.x).toBeGreaterThanOrEqual(0);
      expect(action.x).toBeLessThan(plan.width);
      expect(action.y).toBeGreaterThanOrEqual(0);
      expect(action.y).toBeLessThan(plan.height);
      expect(action.duration).toBeGreaterThan(0);
      expect(action.at + action.duration).toBeLessThan(plan.duration_seconds);
    }
  });

  it("clicks and verifies every dashboard section", () => {
    expect(plan.actions.filter((action) => action.click).map((action) => action.label)).toEqual([
      "copy-openclaw",
      "copy-rollback",
      "fingerprints",
      "reachability",
      "coverage",
    ]);
    for (const section of ["fingerprints", "reachability", "coverage"] as const) {
      const action = plan.actions.find((candidate) => candidate.label === section);
      const segment = plan.segments[section];
      expect(action?.expect_hash).toBe(`#${section}`);
      expect(action?.expect_text).toBeTruthy();
      expect(action?.at).toBeLessThanOrEqual(segment.start);
      expect(segment.start - ((action?.at ?? 0) + (action?.duration ?? 0))).toBeLessThanOrEqual(
        0.25,
      );
    }
  });

  it("targets alert controls by DOM evidence instead of stale coordinates", () => {
    const openClaw = plan.actions.find((action) => action.label === "copy-openclaw");
    const miniShai = plan.actions.find((action) => action.label === "copy-rollback");
    expect(openClaw?.target_text).toBe("@goodman-demo/calendar-sync");
    expect(openClaw?.control_text).toBe("Copy rollback template");
    expect(miniShai?.target_text).toBe("mini-shai-hulud-loader");
    expect(miniShai?.control_text).toBe("Copy rollback template");
  });

  it("records a 1080p 30 fps canonical asset", () => {
    expect(plan.output).toBe("goodman_walkthrough.mp4");
    expect(plan.width).toBe(1920);
    expect(plan.height).toBe(1080);
    expect(plan.fps).toBe(30);
    expect(plan.duration_seconds).toBeGreaterThanOrEqual(21);
    expect(plan.duration_seconds).toBeLessThanOrEqual(24);
  });

  it("ships an exact-frame canonical walkthrough", () => {
    const recording = resolve(__dirname, "..", "recordings", plan.output);
    const probe = JSON.parse(
      execFileSync(
        "ffprobe",
        [
          "-v",
          "error",
          "-select_streams",
          "v:0",
          "-count_frames",
          "-show_entries",
          "stream=codec_name,width,height,r_frame_rate,avg_frame_rate,nb_read_frames:format=duration",
          "-of",
          "json",
          recording,
        ],
        { encoding: "utf8" },
      ),
    ) as {
      streams: Array<{
        codec_name: string;
        width: number;
        height: number;
        r_frame_rate: string;
        avg_frame_rate: string;
        nb_read_frames: string;
      }>;
      format: { duration: string };
    };
    const stream = probe.streams[0];

    expect(stream.codec_name).toBe("h264");
    expect([stream.width, stream.height]).toEqual([plan.width, plan.height]);
    expect(stream.r_frame_rate).toBe(`${plan.fps}/1`);
    expect(stream.avg_frame_rate).toBe(`${plan.fps}/1`);
    expect(Number(stream.nb_read_frames)).toBe(plan.fps * plan.duration_seconds);
    expect(Number(probe.format.duration)).toBeCloseTo(plan.duration_seconds, 2);
  });
});
