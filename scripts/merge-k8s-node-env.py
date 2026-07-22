#!/usr/bin/env python3
"""Build a JSON Patch that enables Goodman on every container without clobbering NODE_OPTIONS."""

import json
import shlex
import sys


def merge_node_options(current: str, required: str) -> str:
    tokens = shlex.split(current)
    for token in shlex.split(required):
        if token not in tokens:
            tokens.append(token)
    return shlex.join(tokens)


def env_patch(container_index: int, env: list[dict], name: str, value: str) -> list[dict]:
    base = f"/spec/template/spec/containers/{container_index}/env"
    for env_index, item in enumerate(env):
        if item.get("name") != name:
            continue
        if item.get("value") == value and "valueFrom" not in item:
            return []
        return [{"op": "replace", "path": f"{base}/{env_index}", "value": {"name": name, "value": value}}]
    return [{"op": "add", "path": f"{base}/-", "value": {"name": name, "value": value}}]


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: merge-k8s-node-env.py NODE_OPTIONS SERVICE", file=sys.stderr)
        return 2
    required_options, service = sys.argv[1:]
    deployment = json.load(sys.stdin)
    containers = deployment.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
    if not containers:
        print("deployment has no containers", file=sys.stderr)
        return 2

    patch: list[dict] = []
    for index, container in enumerate(containers):
        env = container.get("env")
        base = f"/spec/template/spec/containers/{index}/env"
        if env is None:
            env = []
            patch.append({"op": "add", "path": base, "value": []})
        current_options = ""
        for item in env:
            if item.get("name") == "NODE_OPTIONS":
                if "valueFrom" in item:
                    print(
                        f"container {container.get('name', index)} uses NODE_OPTIONS from valueFrom; "
                        "set GOODMAN_NODE_OPTIONS explicitly instead of overwriting it",
                        file=sys.stderr,
                    )
                    return 2
                current_options = item.get("value", "")
                break
        patch.extend(env_patch(index, env, "NODE_OPTIONS", merge_node_options(current_options, required_options)))
        patch.extend(env_patch(index, env, "GOODMAN_SERVICE", service))

    print(json.dumps(patch, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
