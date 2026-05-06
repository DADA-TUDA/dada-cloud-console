// TypeScript types matching the Go backend models.

export type UserRole =
  | "platform_admin"
  | "project_owner"
  | "developer"
  | "viewer";

export interface User {
  id: string;
  email: string;
  display_name: string;
  role: UserRole;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  slug: string;
  description: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface Environment {
  id: string;
  project_id: string;
  name: string;
  slug: string;
  cluster: string;
  namespace: string;
  created_at: string;
  updated_at: string;
}

export type OperationStatus =
  | "Created"
  | "Validated"
  | "Queued"
  | "Rendering"
  | "CommittingToGit"
  | "Committed"
  | "WaitingForArgoSync"
  | "Syncing"
  | "Reconciling"
  | "Ready"
  | "Failed"
  | "Cancelled"
  | "WaitingForApproval";

export type OperationKind =
  | "CreateServiceDatabase"
  | "UpdateServiceDatabase"
  | "DeleteServiceDatabase"
  | "CreateApplication"
  | "UpdateApplication"
  | "DeleteApplication"
  | "CreateServiceEndpoint"
  | "UpdateServiceEndpoint"
  | "DeleteServiceEndpoint";

export interface Operation {
  id: string;
  project_id: string;
  environment_id: string;
  kind: OperationKind;
  status: OperationStatus;
  resource_id: string;
  resource_type: string;
  git_commit_sha?: string;
  error_message?: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ServiceDatabase {
  id: string;
  name: string;
  project_id: string;
  environment_id: string;
  engine: string;
  version: string;
  storage_gb: number;
  replicas: number;
  status: OperationStatus;
  created_at: string;
  updated_at: string;
}

export interface ApiError {
  error: string;
}
