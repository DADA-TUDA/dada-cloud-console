# Current MVP assessment and next steps

## What exists now

The current MVP has already proven the most important product hypothesis:

```text
UI click → operation → generated ServiceDatabase YAML → Git state repo
```

Observed generated file:

```text
clusters/beget-prod/projects/client-a/environments/prod/apps/profi-backend/database.yaml
```

Generated resource:

```yaml
apiVersion: platform.dada-tuda.ru/v1alpha1
kind: ServiceDatabase
metadata:
  name: profi-db
  namespace: client-a-prod
  labels:
    dada.io/project: client-a
    dada.io/environment: prod
    dada.io/operation: 0472db45-5016-4aad-87e3-10ada60b739f
spec:
  appRef:
    name: profi-backend
  engine: postgres
  database: profi
  backup:
    enabled: true
    schedule: daily
    retention: 7d
```

This is already the correct direction.

## What is good

- The UI already has a cloud-console shape.
- The project model exists.
- Environment context exists.
- Operation history exists.
- The first product action exists: CreateServiceDatabase.
- Desired state lands in a Git repo.
- Resource labels include project, environment and operation.
- The generated manifest is product-level, not raw Kubernetes infrastructure.

## What is not production-ready yet

### 1. State repo bootstrap is incomplete

`client-a` has generated application resources, but no `project.yaml` is shown under `client-a`.

Need to define whether project/environment records are:

- DB-only;
- Git-only;
- both DB and Git.

Recommended: both.

### 2. No Argo integration shown yet

The MVP generated Git files, but next step is to make Argo consume this repo path.

Need:

- ApplicationSet or App-of-Apps for `clusters/beget-prod/projects/*/environments/*`;
- health/sync status back into console.

### 3. No real Kubernetes status aggregation yet

The UI shows Ready, but currently this may be simulated.

Need real status from:

- `ServiceDatabase.status`;
- Argo Application status;
- Kubernetes events.

### 4. Operation idempotency must be hardened

The worker must handle retries and duplicate clicks.

### 5. Backend secrets and Git credentials must be moved to Kubernetes Secret

No credentials in values committed to Git.

### 6. DB migrations must be production-safe

Need a migration job or controlled startup migration process.

## Immediate next steps

### Step 1: Freeze the current MVP as v0.1

Tag it:

```bash
git tag dada-console-v0.1-mvp
git push origin dada-console-v0.1-mvp
```

### Step 2: Add tests around current behavior

Minimum:

- CreateServiceDatabase API test;
- worker renders expected YAML;
- duplicate operation does not create duplicate files;
- invalid name is rejected;
- user cannot create resource in another project.

### Step 3: Install console into cluster

Use provided Helm chart.

### Step 4: Wire Argo to generated state repo

Create an Argo ApplicationSet that watches:

```text
clusters/beget-prod/projects/*/environments/*/apps/*
```

or a simpler app that points to:

```text
clusters/beget-prod/projects
```

### Step 5: Replace simulated status with real status

Backend should read:

```text
ServiceDatabase/client-a-prod/profi-db
```

and map `.status.conditions` to UI status.

### Step 6: Add App resource next

Current database references `appRef: profi-backend`, but there is no `App` manifest in the generated tree.

Next product flow must be:

```text
Create App → Add Database
```

not standalone database only.

## Recommended next feature order

1. Real cluster install.
2. Real Argo sync.
3. Read real ServiceDatabase status.
4. Generate `App` manifest.
5. Create endpoint/domain.
6. Add operation retry/idempotency hardening.
7. Add admin debug page.
8. Add client-safe quotas.
