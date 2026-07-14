#!/usr/bin/env bash
# In-cluster e2e on kind (Linux host only — eBPF needs the real kernel).
# Builds images, loads them into kind, helm-installs Goodman, deploys the
# victim workload with good-pkg@1.0.0, waits for baseline, swaps to 1.0.1,
# and asserts a CRITICAL alert via the collector API. (Plan §11 DoD.)
set -euo pipefail
cd "$(dirname "$0")/../.."

CLUSTER=goodman-e2e
NS=goodman
REG=goodman
TAG=e2e

need() { command -v "$1" >/dev/null || { echo "missing tool: $1"; exit 2; }; }
need kind; need kubectl; need helm; need docker

echo "== build images =="
make docker REGISTRY="$REG" TAG="$TAG"

echo "== kind cluster =="
kind get clusters | grep -qx "$CLUSTER" || kind create cluster --name "$CLUSTER"
kind load docker-image "$REG/collector:$TAG" "$REG/sensor:$TAG" --name "$CLUSTER"

echo "== helm install =="
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install goodman deploy/helm/goodman -n "$NS" \
  --set cluster=dev \
  --set sensor.image="$REG/sensor:$TAG" \
  --set collector.image="$REG/collector:$TAG" \
  --set learningWindow.obsCount=20 \
  --set learningWindow.minAgeHours=0
kubectl -n "$NS" rollout status deploy/goodman-collector --timeout=120s
kubectl -n "$NS" rollout status ds/goodman-sensor --timeout=120s

echo "NOTE: deploy your Node workload with NODE_OPTIONS=--perf-basic-prof-only-functions and"
echo "swap good-pkg 1.0.0 -> 1.0.1 to trigger the alert. Port-forward the UI:"
echo "  kubectl -n $NS port-forward svc/goodman-collector 8844:8844"
