# ADR-007: Portainer Edge Agent as Remote Docker Runtime Layer

## Status

Proposed — 2026-05-13

## Context

DADA Cloud v2 needs to deploy and manage Docker Compose workloads on customer-provisioned VMs (VDS).
The system must support:

- remote Docker runtime access over untrusted/NATted networks
- Docker Compose stack deployment and lifecycle management
- real-time log streaming and container stats
- multi-tenant isolation (one environment per customer VM)
- future replaceability of the runtime layer without rewriting the control plane

Three runtime approaches were evaluated:

| Option | Pros | Cons |
|--------|------|------|
| Custom SSH + Docker API proxy | Full control | Must build tunneling, reconnect, auth from scratch |
| Coolify / Dokku embedded | Feature-rich PaaS | UI-first, hard to embed as subsystem, opinionated |
| **Portainer CE + Edge Agent** | API-first, agent handles tunnel/reconnect, proven in prod | One more infra component to operate |
| Nomad client | Production-grade, flexible | Compose UX poor, higher complexity, overkill for v2 |

The key insight: the problem of secure remote Docker management is **solved** by Portainer Edge Agent.
Building a custom solution duplicates that work without strategic advantage.

## Decision

Use **Portainer CE** (self-hosted) as the runtime execution layer, with **Portainer Edge Agent** deployed
on every customer VM via cloud-init.

### Architecture

```
User (Panel UI)
      │
      ▼
DADA Go Panel   ←── Crossplane XR API (AppServer / AppDeployment)
      │
      ▼
Portainer CE API  (central, runs in dada-cloud cluster)
      │  REST / WebSocket
      ▼
Portainer Edge Agent  (runs on each customer VM, initiates outbound tunnel)
      │
      ▼
Docker Engine (on customer VM)
```

### Provisioning flow

1. User creates `AppServer` via Panel UI.
2. Panel generates Crossplane `AppServer` XR.
3. Crossplane + provider-terraform provisions VDS (Beget / OpenStack).
4. **cloud-init** on the VM installs Docker + Portainer Edge Agent, registers with central Portainer.
5. Edge Agent initiates outbound WebSocket tunnel to Portainer — no inbound firewall rules needed.
6. Panel calls Portainer API to:
   - verify environment is online
   - deploy Docker Compose stack (`AppDeployment`)
   - stream logs, read container stats, restart containers, update env vars

### What DADA builds (our code)

| Layer | What we write |
|-------|---------------|
| Control plane API | `AppServer`, `AppDeployment` Crossplane XRDs |
| VM provisioning | Terraform/OpenTofu modules (VDS lifecycle) |
| cloud-init template | Docker + Edge Agent install + registration token |
| Glue service | Go service: XR → Portainer API calls (register env, create stack) |
| Panel UI/API | Auth, tenancy, billing/quotas, abstraction over Portainer API |

### What we do NOT build

- Docker orchestration engine
- Compose execution
- Log streaming
- Remote runtime / tunneling
- Agent reconnect logic
- State sync

## Consequences

### Positive

- **Drastically reduced scope**: runtime layer is ~0 lines of custom code for v2.
- **Proven reliability**: Portainer Edge Agent handles NATted VMs, reconnects, encrypted tunnels.
- **API-first**: Portainer REST API covers stacks, environments, logs, containers, images, events.
- **Replaceability**: Crossplane XR + Terraform infra layer is runtime-agnostic.
  Portainer can be swapped for Nomad, k3s, or a custom runtime later without touching the control plane.
- **Operational familiarity**: Portainer UI remains available as a debugging escape hatch.

### Negative / Risks

| Risk | Mitigation |
|------|------------|
| Portainer API coverage gaps | Evaluate API surface before v2 GA; document escape hatches |
| Portainer CE license changes (AGPL) | Pin version; evaluate BE (Business Edition) if scale requires |
| Single Portainer instance = SPOF | Run Portainer in HA mode (replicas + shared DB) for prod |
| Edge Agent version drift on VMs | Pin agent version in cloud-init; automate upgrade via Portainer API |

## Alternatives Considered

### Custom SSH + Docker API proxy
Rejected. Requires building: secure tunnel, key management, reconnect logic, agent lifecycle — all
solved problems that Portainer Edge Agent handles today.

### Coolify
Rejected. Designed as a standalone PaaS with its own UI and tenant model. Embedding it as a subsystem
under our control plane would require fighting its architecture at every layer.

### Nomad
Noted as a future migration path. Better for heterogeneous workloads and multi-region scheduling,
but compose-first UX is inferior. Portainer is the right starting point.

## Related ADRs

- ADR-006: v1 Complete — v2 Scope Opened (establishes that v2 targets VM-based deployments)

## References

- Portainer API docs: https://docs.portainer.io/api/docs
- Portainer Edge Agent: https://docs.portainer.io/admin/environments/add/edge
- Crossplane XRDs: https://docs.crossplane.io/latest/concepts/composite-resource-definitions/
