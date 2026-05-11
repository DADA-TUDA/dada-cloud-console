export type MemberRole = "platform-admin" | "developer" | "client-admin" | "client-viewer";

export type OperationStatus =
  | "Created" | "Validated" | "Queued" | "Rendering"
  | "CommittingToGit" | "Committed" | "WaitingForArgoSync"
  | "Syncing" | "Reconciling" | "Ready" | "Failed"
  | "Cancelled" | "WaitingForApproval";

export interface User {
  id: string;
  username: string;
  email: string;
  display_name: string;
}

export interface Project {
  id: string;
  name: string;
  display_name: string;
  owner_type: string;
  default_environment: string;
  created_at: string;
  updated_at: string;
  role?: MemberRole;
}

export interface Environment {
  id: string;
  project_id: string;
  name: string;
  namespace: string;
  type: "dev" | "prod";
  created_at: string;
}

export interface Operation {
  id: string;
  actor_id: string;
  project_id: string;
  environment_id?: string;
  action: string;
  resource_kind: string;
  resource_name: string;
  status: OperationStatus;
  payload?: Record<string, unknown>;
  git_commit?: string;
  git_path?: string;
  argo_application?: string;
  error_code?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

export interface ResourceSnapshot {
  id: string;
  project_id: string;
  environment_id?: string;
  kind: string;
  name: string;
  phase?: string;
  summary_json: Record<string, unknown>;
  last_synced_at: string;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export interface ProjectsResponse {
  projects: Project[];
}

export interface ProjectDetailResponse {
  project: Project;
  environments: Environment[];
  role: MemberRole;
}

export interface DatabasesResponse {
  databases: ResourceSnapshot[];
}

export interface CreateDatabaseResponse {
  operation: Operation;
  message: string;
}

export interface OperationsResponse {
  operations: Operation[];
}

export interface AppSummary {
  image: string;
  port: number;
  replicas: number;
  profile: string;
  status: string;
  message: string;
}

export interface AppsResponse {
  apps: ResourceSnapshot[];
}

export interface CreateAppResponse {
  operation: Operation;
  message: string;
}

export interface DeployImageResponse {
  operation: Operation;
  message: string;
}

export interface EndpointSummary {
  app_name: string;
  fqdn: string;
  auth_enabled: boolean;
  auth_scheme: string;
  swagger_enabled: boolean;
  status: string;
  message: string;
}

export interface EndpointsResponse {
  endpoints: ResourceSnapshot[];
}

export interface CreateEndpointResponse {
  operation: Operation;
  message: string;
}
