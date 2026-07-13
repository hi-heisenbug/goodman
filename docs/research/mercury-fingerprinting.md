# Research: Mercury's fingerprinting, and what Goodman can steal from it

Status: RESEARCH ONLY. No code. Written 2026-07-13.
Source studied: `~/Desktop/a/startup/Heisenbug/code/mercury` (Cisco's open-source
network metadata capture + fingerprinting engine, GPL). Key specs live in that
repo under `doc/npf.md`, `doc/wnb.md`, `doc/fdc.md`, `doc/resources.md`. The
classifier math is published as "Accurate TLS Fingerprinting using Destination
Context and Knowledge Bases" (arXiv:2009.01939).

## 0. TL;DR

Mercury and Goodman solve mirror-image problems:

| | Mercury | Goodman |
|---|---|---|
| Vantage | On the network (packets) | On the host (eBPF + stacks) |
| Fingerprints | Protocol messages (TLS ClientHello, QUIC, HTTP, SSH, TCP SYN) | Behavior sets per `(service, package, version)` |
| Attribution question | "Which *process/app* sent this flow?" (inferred, probabilistic) | "Which *package* made this syscall?" (observed, ground truth) |
| Attribution mechanism | Weighted naive Bayes over fingerprint + destination context, against a curated knowledge base | Stack walk -> perf map / proc maps -> deepest `node_modules` frame |
| Unknown handling | `randomized` bucket, aggregate classifier | `<app>` / `<unknown>` sentinels, hard ladder |

Mercury has to *guess* the process because it can't see the host; Goodman *knows*
the package because it is the host. What mercury built to compensate for not
having ground truth is exactly the machinery Goodman needs for the situations
where its ground truth runs out (unattributed events, cross-customer baselines,
alert ranking, and enriching bare `ip:port` connects). The five concrete
takeaways, in priority order:

1. **Capture TLS ClientHello (SNI + client fingerprint) at connect time** so
   CONNECT behaviors become `CONNECT api.stripe.com:443` instead of
   `CONNECT 54.187.x.x:443`. Mercury's degrease/normalize spec is the recipe.
2. **Equivalence classes for destinations** (IP -> ASN, SNI -> eTLD+1 domain)
   instead of, or in addition to, our current public-CIDR aggregation.
3. **A versioned, normalized, reversible canonical form** for behaviors and
   behavior-set fingerprints (NPF's design lessons: version the ruleset, sort
   for permutation invariance, collapse unknowns to sentinels, keep it
   reversible).
4. **A probabilistic scorer with a curated knowledge base** for the two places
   hard attribution fails: soft attribution of `<unknown>` events, and rarity
   scoring of new behaviors for alert ranking. Mercury's weighted naive Bayes
   is small, fast, and fully reimplementable (math reproduced in section 4).
5. **The resource-archive distribution model**: a versioned tarball of curated
   knowledge (per-package behavior priors, watchlists, ASN db) shipped to
   collectors. Our `Fingerprint.Origin = imported` multi-cluster import is the
   embryo of this.

---

## 1. What mercury is

Mercury captures packets (AF_PACKET TPACKET_V3 zero copy), identifies protocols
by byte patterns on the first packet of a flow, selectively parses them with
zero heap allocation, extracts a **fingerprint string** plus **destination
context** (dst IP, dst port, SNI, user agent), and runs a classifier that
outputs: most probable process, malware probability, score, and OS prevalence.
Everything reusable lives in `libmerc.so`; `pmercury` is a Python binding over
the same core.

The pipeline shape (capture -> parse -> normalize -> fingerprint -> classify
against knowledge base -> JSON) is worth internalizing because Goodman's
pipeline is structurally identical with different substrates:

```
mercury:  packet    -> parse proto -> normalize/degrease -> fp string        -> WNB vs fpdb    -> process guess + score
goodman:  raw event -> stack walk  -> canonicalize args  -> behavior string  -> set-diff vs     -> drift alert (binary)
                                                                                learned baseline
```

Mercury's third stage (normalize) and fifth stage (probabilistic scoring
against a shared knowledge base) are where Goodman is currently weakest.

## 2. How mercury constructs fingerprints (NPF)

The canonical format, "Network Protocol Fingerprinting" (`doc/npf.md`), is an
ordered tree of byte strings: hex for bytes, `(...)` for ordered lists, `[...]`
for **sorted** lists. Example TLS fingerprint:

```
tls/2/(0301)(c02bc02f...)[(0000)(000a...)(0010...)...]
```

Design properties that matter to us:

- **Versioned rulesets.** `tls`, `tls/1`, `tls/2` are three different
  normalization rules over the same message. When Chrome started permuting
  ClientHello extensions, mercury shipped `tls/1` (sort all extensions
  lexicographically) and later `tls/2` (keep only a curated include-list of
  extensions in fixed slots) without invalidating old databases. The version
  is part of the fingerprint string itself.
- **Normalization defeats randomization.** GREASE values (`0x?a?a`) are all
  mapped to `0a0a` ("degreasing"); randomized extension order is defeated by
  sorting; unknown private extensions are collapsed to a single sentinel value
  (65280), unknown unassigned ones to 62. The goal is that two runs of the
  same client produce the same string even when the client randomizes.
- **Reversible, not a hash.** Unlike JA3/JA4, the string preserves the data
  (you can read the ciphersuites back out). SHA-256 of the string is used as
  a compact nickname when needed. Reversibility enables **prefix matching**
  (truncated messages) and **approximate matching** (edit distance), both
  specified in NPF.
- **Well-formedness enforced centrally.** One `class fingerprint` builder
  validates balance/hex and discards truncated fingerprints rather than
  emitting garbage (`src/libmerc/fingerprint.h`). Never-emit-wrong over
  always-emit, same philosophy as our never-misattribute rule.

Per-protocol construction, briefly (all in `src/libmerc/`):

- **TLS** (`tls.h`): `(legacy_version)(degreased ciphersuites)[normalized extensions]`.
- **QUIC** (`quic.h`): decrypts Initial packets, reassembles CRYPTO frames,
  then fingerprints the inner ClientHello with the QUIC version prepended.
- **HTTP** (`http.cc`): `(method)(protocol)(selected headers)`, with a fixed
  include-list: 7 headers contribute name+value (accept, accept-encoding,
  connection, dnt, dpr, upgrade-insecure-requests, x-requested-with), 10
  contribute name only (user-agent, host, accept-language, ...).
- **SSH** (`ssh.h`): the KEXINIT name-lists verbatim, plus the id string.
- **TCP/IP** (`tcpip.h`, `ip.h`): SYN options + window + TTL class, i.e.
  OS-stack fingerprinting (p0f style).
- **DNS**: metadata only, deliberately not fingerprinted.

## 3. Destination context (FDC)

A fingerprint alone is weak evidence (thousands of apps link the same TLS
stack; every Node app on earth shares one TLS fingerprint). Mercury's insight,
and the core of the arXiv paper, is that **fingerprint + destination context**
is strong evidence. An FDC record is:

```
[fingerprint, server_name, dst_ip, dst_port, user_agent, ?truncation]
```

Raw destination values are generalized through **equivalence classes** before
scoring:

- dst IP -> **ASN** (via an embedded lctrie `pyasn.db`), plus raw IP kept as a
  second feature
- server_name -> **second-level domain (eTLD+1)**, plus full normalized SNI
- dst port kept raw
- user agent kept raw

This matters for us directly: Goodman's `aggregateConnect` public-CIDR
collapsing is a cruder version of the IP -> ASN class, and we have no SNI at
all today (section 6.1).

## 4. Mercury's classifier: weighted naive Bayes (reimplementable spec)

Implementation: `src/libmerc/naive_bayes.hpp`, `analysis.h`, `softmax.hpp`.
Everything is precomputed at database-load time; classification is a handful
of hash lookups and float adds.

**Knowledge base** (`fingerprint_db.json`, one entry per fingerprint string):

```json
{
  "str_repr": "tls/2/(0301)...",
  "total_count": 12345,
  "process_info": [
    {
      "process": "(chrome.exe)(chrome.exe)",
      "count": 9876,
      "malware": false,
      "classes_ip_as":            {"AS15169": 5000, ...},
      "classes_ip_ip":            {"142.250.1.1": 120, ...},
      "classes_hostname_domains": {"google.com": 4000, ...},
      "classes_hostname_sni":     {"www.google.com": 3500, ...},
      "classes_port_port":        {"443": 9800, ...},
      "classes_user_agent":       {...},
      "os_info": {"Windows 10": 8000, ...}
    }
  ]
}
```

**Feature weights** (defaults, tunable per fingerprint in the DB):

```
as = 0.13924, domain = 0.15590, port = 0.00528, ip = 0.56735, sni = 0.96941, ua = 1.0
```

Note what the weights say: SNI and user agent dominate, raw IP is strong,
port is nearly useless. These were fit on ground truth.

**Precomputation at load, per fingerprint entry:**

```
base_prior       = log(0.1 / total_count)
proc_prior       = log(0.1)
prior[p]         = max(log(count_p / total_count), proc_prior) + base_prior * sum(weights)

for each feature f, observed value v with count c under process p:
    update[f][v] += (p, (log(c / total_count) - base_prior) * weight_f)
```

Storing updates keyed by feature value (not by process x feature) is the trick
that makes classification O(#features) instead of O(#processes x #features).

**Classification of an event (fp, asn, domain, port, ip, sni, ua):**

```
score[p] = prior[p]                          for all p in the entry
for each feature f:
    for (p, delta) in update[f][observed_value_f]:  score[p] += delta
softmax over score[] (subtract max before exp, standard log-sum-exp)
answer   = argmax;  confidence = exp(max)/sum;  p_malware = sum over malware-labeled processes
```

Two refinements worth copying:

- **"generic dmz process" rule**: fingerprints with no ground truth get a
  placeholder top process; if it wins, report the runner-up instead. A clean
  way to encode "we know this fingerprint exists but not what it is."
- **`randomized` bucket**: a fingerprint absent from both the DB and the
  prevalence list is classified against a synthetic `tls/<ver>/randomized`
  aggregate entry, and the status is reported honestly as
  `randomized_fingerprint`, not silently misclassified.

**Status taxonomy**: every result carries `labeled` / `unlabeled` /
`randomized`, driven by two curated artifacts: the fpdb and a
`fp_prevalence_tls.txt` top-N list. This three-way honesty label is a nicer
UX than a bare confidence float.

## 5. Ground truth and knowledge-base curation

This is the part of mercury that looks most like Goodman. The fpdb is built
from **on-host ground truth**:

- `python/mercury_network_monitor.py`: runs libmerc on live traffic and joins
  each flow to the owning process via `psutil.net_connections()`
  (5-tuple -> pid) plus pid -> {process name, exe sha256, path, parent, OS}.
- `python/build_mercury_resources.py`: aggregates those labeled records into
  the fpdb JSON (the count dictionaries above), emits the prevalence list and
  watchlists, and tars everything into a versioned resource archive
  (`resources.tgz`: VERSION, fingerprint_db.json.gz, fp_prevalence_tls.txt,
  doh-watchlist.txt, pyasn.db; optionally AES-encrypted).

So Cisco's telemetry loop is: sensors with host visibility produce labeled
FDC records -> offline aggregation -> versioned knowledge base -> shipped to
network sensors that have no host visibility. **Goodman sensors are strictly
better ground-truth collectors than mercury's monitor**: we label flows not
just with the process but with the `package@version` that initiated them.
That is a dataset nobody else has, and it compounds across customers.

## 6. What Goodman should take (concrete proposals)

### 6.1 SNI capture at connect time (highest value, do first)

Today `EVENT_NET_CONNECT` renders `ip:port`, and canonicalization can only
aggregate by CIDR. Mercury demonstrates that **SNI is the single most
predictive destination feature** (weight 0.97 vs 0.14 for ASN). For us it is
even better than a feature: `CONNECT api.stripe.com:443` is a human-readable,
CDN-stable behavior, while `CONNECT 54.187.x.x:443` churns with every DNS
rotation and poisons baselines with noise.

Sketch (research-level, not a plan): after `connect()`, the first
`sendmsg/write` on that socket carries the TLS ClientHello for TLS flows. An
eBPF hook on `sys_enter_sendto`/`sendmsg` (or a tc/sockops program) can grab
the first N bytes per socket, once, for watched pids. Userspace then parses
the ClientHello. Mercury's parsing is ~200 lines of the relevant logic
(`tls.h`: record header, handshake header, extensions walk, find ext 0x0000
server_name); we would reimplement in Go, not link GPL libmerc. The same
capture gives us the client fingerprint bytes for free (6.3). Fallback for
non-TLS or missed captures: keep `ip:port` exactly as today. Alternative
cheaper path: hook `getaddrinfo`/DNS via uprobe or dns snooping and keep a
pid-local `ip -> name` cache; less exact, no parsing, worth comparing.

Payoff: behaviors become semantically stable, baselines shrink, the
`new-outbound-connect` rule fires on *domains* (what a security analyst
actually wants to see), and exfil watchlists (6.5) become possible.

### 6.2 Equivalence classes for destinations

Adopt mercury's two-level generalization in `canonical.go`'s connect path:

- IP -> ASN (or org), via an embedded MaxMind-style or pyasn-style db in the
  collector, applied at canonicalization or at diff time.
- SNI -> eTLD+1 (use `golang.org/x/net/publicsuffix`), keeping full SNI as
  evidence detail.

Then a baseline can contain `CONNECT domain:github.com:443` while evidence
retains the exact SNI and IP. This is a strictly more principled version of
the current public-CIDR aggregation and can replace it behind a flag.

### 6.3 TLS client fingerprint as a drift signal (cheap add-on to 6.1)

All Node code shares Node's TLS stack, so a per-package TLS fingerprint is
mostly constant... which is exactly why it is a good tripwire. If a package
suddenly produces a *different* client fingerprint (bundled static binary,
spawned curl/python, raw TLS-in-JS), that is a strong anomaly. Storage cost:
one short string per fingerprint row. We should use mercury's NPF `tls/2`
normalization rules (degrease, sort, collapse unknowns) so the string is
stable across Node versions' GREASE randomization. Also gives us a
`fake-TLS` style check for malware using homegrown TLS.

### 6.4 A versioned canonical form for behaviors ("NBF")

Our behavior strings (`CONNECT 1.2.3.4:443`, `READ /app/node_modules/x/**`)
are already a canonical form, but the *rules* are implicit in `canonical.go`
and unversioned. NPF's lesson: **put the ruleset version in the string or in
the fingerprint metadata**, because normalization rules will change (6.1 and
6.2 both change them) and old baselines must not silently mismatch new
events. Proposal: a `canon/2` style version stamped on `Fingerprint`, with
the diff engine refusing to compare across versions (force relearn or run a
migration mapping). Cheap to add now, painful to retrofit after the first
customer has 90 days of baselines.

Also worth copying: NPF's `[...]` sorted-list convention. A behavior *set*
serialized sorted is a stable, hashable identity for a whole
`(package, version)` profile, useful for cross-cluster baseline dedup and
for a compact "profile hash" in the digest email.

### 6.5 Watchlists as a first-class curated artifact

Mercury ships `doh-watchlist.txt` inside the resource archive and tags flows
with attributes (DoH, domain-faking, fake-TLS, encrypted-channel) orthogonal
to the process guess. Goodman analog, trivially buildable once SNI exists:
a curated list of exfil-associated destinations (pastebin.com, transfer.sh,
webhook.site, burpcollaborator, interactsh, telegram bot API, discord
webhooks, ngrok, ...) that makes a CONNECT AlwaysOn regardless of baseline
state, exactly like our `cloud-metadata` rule. This is high-signal, cheap,
and demo-friendly (the replay corpus attacks all exfiltrate somewhere).

### 6.6 Probabilistic scoring where hard attribution ends

Keep the hard ladder (never misattribute), but add mercury-style soft
inference in two bounded places:

1. **Soft attribution of `<unknown>` events.** When no dependency frame
   resolves, we currently emit `<app>`/`<unknown>`. A WNB-style scorer could
   say "given this service's fingerprint DB, behavior `CONNECT registry.npmjs.org:443`
   has P=0.93 of belonging to `npm-registry-fetch`" and surface it as a
   *suggestion* with a confidence float, clearly labeled, never written into
   the baseline. Mercury's `labeled/unlabeled/randomized` status taxonomy is
   the right UX: our analog is `attributed / suggested / unknown`.
2. **Rarity scoring for alert ranking.** Mercury's prevalence list
   (top-1000 fingerprints) separates "common but unlabeled" from "never seen
   anywhere". Once >1 cluster/customer shares baselines, a global prevalence
   table over behaviors (keyed by equivalence classes from 6.2) lets us rank
   a drift alert by how unusual the behavior is *globally*, not just for this
   service. "First time `lodash@4.17.21` has connected to an AS in country X
   across all deployments" is a much better alert title.

The math in section 4 is sufficient to implement both; total state is one
count table per (package, behavior-class) and the log-space fold is trivial
in Go. Feature weights can start at mercury's defaults and be refit later.

### 6.7 The resource archive model for cross-customer knowledge

Mercury's distribution unit is a versioned, optionally encrypted tarball:
VERSION + fpdb + prevalence + watchlists + ASN db, loaded atomically, with
the VERSION qualifier controlling thresholds. Goodman already has the seed of
this (`Fingerprint.Origin = imported`, migration 006, multi-cluster baseline
sharing). The mature form is a signed "goodman intelligence pack":

```
goodman-pack.tgz
  VERSION                      (date + lite/full qualifier)
  package_priors.json.gz       (per package@version: expected behavior classes + counts)
  prevalence.txt               (global behavior-class counts)
  watchlist-exfil.txt          (6.5)
  asn.db                       (6.2)
```

collectors load it read-only, tag anything derived from it as
`origin: imported`, and never let it write baselines. This is also the
monetizable network effect: every pilot's (consented, anonymized) fingerprints
improve the pack, the pack makes day-1 alerts possible for the next customer
(no 24h learning window for well-known packages), and it is the same
"knowledge base as moat" play mercury/Cisco run.

### 6.8 Smaller design lessons

- **Central fingerprint builder with well-formedness validation**: one place
  that constructs behavior strings and rejects malformed/truncated input,
  instead of `fmt.Sprintf` scattered per event type. We are close already.
- **Truncation is encoded, not hidden**: FDC records carry a truncation code.
  Analog: when a stack was truncated at MAX_STACK_DEPTH or a perf-map lookup
  was stale, record it on the event; it changes how much to trust the
  attribution and is free to capture.
- **DNS deliberately not fingerprinted**: mercury extracts DNS as metadata
  but never classifies on it. Same discipline for us: high-churn signals
  (ephemeral ports, DNS query IDs) belong in evidence, never in baselines.
- **Softmax with log-sum-exp and precomputed updates**: if 6.6 ships, copy
  the numerics (subtract max before exp, precompute per-value updates at
  load) rather than rediscovering the stability/perf issues.

## 7. What NOT to take

- **Packet capture / on-wire parsing as a vantage point.** Mercury exists
  because it can't see the host. We can. Full-flow capture (AF_PACKET, flow
  tables, reassembly) would blow our <1% CPU budget and duplicates what the
  kernel already tells us. We take only the one-shot ClientHello grab (6.1).
- **JA3/JA4 compatibility.** Mercury's own docs explain why hashed
  fingerprints are inferior (irreversible, not normalizable, fragmented by
  GREASE). If we emit TLS fingerprints at all, emit NPF-style reversible
  strings; add a JA4 column later only if a customer integration asks.
- **The full protocol zoo** (SSH, DHCP, STUN, OpenVPN, tofsee, QUIC
  decryption). Node/Python workloads talking TLS cover the wedge. QUIC/HTTP3
  ClientHello capture is real future work (undecryptable without mercury-style
  Initial-packet crypto) but not now.
- **GPL code.** Mercury is GPL; we reimplement ideas (formats, math, curation
  workflow), we do not link or port code. The NPF spec and the arXiv paper
  are the clean-room references.
- **OS identification** (bag-of-fingerprints logistic regression per source
  IP): interesting, irrelevant to our problem.

## 8. Suggested sequencing (if/when we build)

1. **6.4 canon versioning** (hours; must land before any normalization change).
2. **6.2 equivalence classes** (days; immediate baseline-noise reduction).
3. **6.1 SNI capture** (1-2 weeks incl. eBPF work; transforms CONNECT quality).
4. **6.5 exfil watchlist** (day; depends on 6.1; big demo win).
5. **6.3 TLS client fingerprint** (days; piggybacks on 6.1's capture).
6. **6.6 rarity scoring / soft attribution** (1-2 weeks; needs >1 deployment's
   data to be honest, so post-pilot).
7. **6.7 intelligence pack** (post-pilot; this is the network-effect moat and
   deserves its own plan).

None of this is committed work; triggers and timelines should be folded into
plan-pilot.md's deferred framework when we decide to act.

## 9. Pointers back into mercury (for future implementers)

- NPF spec: `doc/npf.md`, CDDL: `doc/npf.cddl`
- Classifier spec: `doc/wnb.md`; FDC record: `doc/fdc.md`
- TLS normalization (degrease, sort, tls/2 include-list): `src/libmerc/tls.h`
  (fingerprint functions ~lines 1418-1732, 1928-1970),
  `src/libmerc/tls_extensions.h`
- WNB implementation: `src/libmerc/naive_bayes.hpp` (priors ~657-663,
  precomputed updates ~103-118, classify ~752-795), scoring
  `src/libmerc/analysis.h` ~222-277, softmax `src/libmerc/softmax.hpp`
- Ground-truth collection: `python/mercury_network_monitor.py`
- Knowledge-base build: `python/build_mercury_resources.py`
- Resource archive format: `doc/resources.md`, `src/libmerc/archive.h`
- Paper: arXiv:2009.01939
