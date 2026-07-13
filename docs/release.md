# Release gate checklist (v0.2.0)

Use this before tagging a minor release that ships HA collector support. Do
**not** skip steps you cannot run — document what was verified and what needs a
human with Postgres/root.

## Automated (no root)

```bash
make vet && make test && make smoke && make replay && make helm-lint
```

All must be green. CI covers SQLite (single-replica path), transactional
`MergeFingerprint` / `UpsertAlert`, and Helm HA template guards.

## Live kernel (root)

```bash
sudo make e2e
```

Confirms the eBPF sensor → collector path on a real kernel. Unprivileged
sandboxes cannot run this; note in the release notes if skipped.

## HA proof (Postgres, human or staging CI)

Two collector replicas behind one Service, shared Postgres DSN:

1. `helm upgrade` with `collector.replicas=2` and `postgres.dsn` set.
2. Run `make replay` (or equivalent load) with sensors posting to the Service.
3. Assert identical final fingerprints on both replicas (REST) and exactly one
   alert per scenario (no duplicate webhooks/digests).
4. Kill one replica mid-ingest; confirm no lost behaviors within spool budget.

This path is not fully automated in CI today; record the environment and outcome
in the release notes when executed.

## Ship

```bash
git tag v0.2.0
# push tag + images per docs/development.md — only when ready
helm upgrade <release> deploy/helm/goodman -f values.yaml
```

Do not tag or push from an agent session unless explicitly requested.
