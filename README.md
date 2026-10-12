# Next Trainer Enterprise

**English** | [中文文档](README.cn.md)

An enterprise-grade training platform for multi-node, multi-GPU workloads:
pooled compute, queued scheduling, per-GPU metering, and centrally managed
datasets — built for algorithm engineering teams running on private
infrastructure.

- Organization: [Next-Lab-AI](https://github.com/Next-Lab-AI)
- Community edition: [wochenlong/lora-scripts-next](https://github.com/wochenlong/lora-scripts-next) (Next Trainer)

> Private repository.

## Overview

Next Trainer Enterprise separates the control plane from the training plane:

- **server** (Go, single binary, embedded Vue 3 web UI) — user system with
  RBAC, resource pools, job queues, dataset management, metering, and the
  presentation layer.
- **agent** (Go, single binary per compute node) — registers itself over an
  outbound long-lived connection, receives instructions, and manages the full
  lifecycle of training processes.
- **vendor/** — pinned, patched, verified source snapshots of training and
  inference engines. Engines are delivered as source and launched as
  processes: config file in, structured JSONL out, exit code to finish.

```
┌─────────────────────────────────────────────┐
│ server (Go control plane, single binary)    │
│  users/RBAC · pools · queues · datasets ·   │
│  metering · web UI (embedded)               │
└──────────────▲──────────────────────────────┘
               │ outbound long connection (agent-pull)
     ┌─────────┴─────────┐─────────────┐
     │ agent (compute)   │  agent      │  agent  ×N
     └─────────┬─────────┘
               │ process launch
        ┌──────┴───────┐
        │ vendor engines│  Python venvs, isolated per engine
        └──────────────┘
```

## Key properties

- **Agent-pull deployment.** Compute nodes only need outbound network access;
  no inbound ports, no firewall changes on the customer side. Registration is
  `agent --server <addr> --token <access-token>`.
- **Zero inbound, no self-upgrade.** The agent listens on nothing and only
  executes jobs assigned by the server. Upgrades are performed manually by
  operations staff — there is no remote bootstrap path in the binary.
- **Resource pools.** Nodes map to pools many-to-many; assignments are
  server-side metadata managed by admins. Users and datasets are authorized
  per pool; queues are per-pool FIFO and dispatch only onto nodes with
  actually idle GPUs.
- **Full user system.** Login, user management, RBAC roles, and revocable
  agent access tokens — administered from the web UI.
- **Central dataset management.** The server owns the dataset registry with a
  pluggable storage backend (local disk by default, S3/MinIO optional).
  Agents pull assets by URI with SHA-256 verification (`file://`, `s3://`,
  or authenticated `https://` from the server). Training artifacts flow back
  through the same storage layer.
- **Per-GPU metering.** Every usage record carries
  `(user, pool, job, node, gpu_index, gpu_model, start, end)` — which card
  ran for how long is first-class data. Pricing is a replaceable policy layer
  for internal showback/chargeback. Server receive-time is authoritative, so
  nodes across time zones are safe.
- **Unified logging.** One JSONL schema
  (`ts/level/source/node_id/job_id/msg`) across agent logs, training stdout,
  and server logs. The protocol log frames are byte-identical to the on-disk
  files, so Filebeat/Logstash can tail them directly.
- **Compute/maintenance lanes.** Downloads, engine installs, and environment
  GC run in the maintenance lane (no GPU); training runs in the compute lane.
  Jobs declare asset dependencies and wait (WAITING state) until their
  datasets are fetched — no job starts without its inputs.
- **Vendor-neutral GPU abstraction.** Metrics and topology go through an
  interface (`Sampler`/`Topology`/`ProcessLister`); NVML is one
  implementation, with room for ROCm and other accelerators.

## Running

Server (control plane):

```sh
server --listen :8080 --db data/server.db --data-root data/
```

Agent (each compute node):

```sh
agent --server wss://ctrl.example.com --token <access-token> \
      --description "DGX Spark dev box" --pool vision
```

`--pool` is only an initial suggestion; admins re-assign nodes to pools
(a node may belong to several) from the web UI. Engine environments are
provisioned per node by admins ("install engine" on the node page) and built
as fingerprint-named, reusable Python venvs.

## Repository layout

```
server/        Go control plane
server/web/    Vue 3 frontend (embedded into the server binary)
agent/         Go agent (compute node)
vendor/        Engine source snapshots (see table below)
docs/design/   Design documents
docs/tasks/    Task books and acceptance reports
docs/api/      Protocol specification (message schemas)
```

## Vendored engine snapshots

`vendor/` contains patched, verified, locally usable source trees, delivered
as-is — no "fetch upstream and patch at install time".

| Directory | Upstream | Pin (snapshot base commit) | Notes |
|---|---|---|---|
| `vendor/sd-scripts` | kohya-ss/sd-scripts | (deeply patched in-tree; no single upstream pin) | Main training engine: SD1.5/SDXL/SD3/Flux/Anima |
| `vendor/ai-toolkit` | ostris/ai-toolkit | `5497a001cb8752c665f93907a0393fc612116fd5` | Install snapshot base (manifest default pin `ecee894e`) |
| `vendor/anima-lora` | sorryhyun/anima_lora | `b43928b5e4b82b907bfca1a322383a33088d0bdd` | Anima LoRA engine |
| `vendor/diffsynth-studio` | modelscope/DiffSynth-Studio | `7686e54d41d25c0e8ed5f1318acc23b6bb832654` | Video/image training suite |
| `vendor/musubi-tuner` | musubi-tuner | `e0cbd8f3dfe38365b10f8bc790b980f8894e8ba1` | Video LoRA engine |
| `vendor/tagger` | lora-scripts-next `mikazuki/tagger` | `ab4acd67dc06e49c5e7b32d18428173a544a8140` | ONNX image tagging (WD14/CL interrogators) |
| `vendor/tag-translation` | lora-scripts-next `mikazuki/tag_translation` | `ab4acd67dc06e49c5e7b32d18428173a544a8140` | Tag translation on llama.cpp (Qwen GGUF) + Chinese dictionary |
| `vendor/next-integrated` | — | — | Next integrated engine (in-house) |

Export convention: copy from the community repo's `extensions/<engine>/source`
(or the tracked tree of `vendor/sd-scripts`), excluding runtime artifacts
(`__pycache__`, `*.egg-info`, `build/`, `jobs/`, `.git`).

## Delivery tiers

Customers choose one of three delivery forms:

1. **Engine source only** — vendored trees for self-integration.
2. **Agent only** — compute-node binary connecting to an existing server.
3. **Full bundle** — server + agent + engines, with an offline-media option
   (source trees + wheelhouse + base models) for air-gapped deployments.

## Documentation

- Design: [docs/design/](docs/design/) — architecture, server↔agent protocol,
  engine process contract, metering & billing, environment provisioning
- Task books: [docs/tasks/](docs/tasks/)
- Protocol specification: [docs/api/](docs/api/)
