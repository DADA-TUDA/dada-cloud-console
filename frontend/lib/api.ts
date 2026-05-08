// API client — fetch wrapper that attaches the JWT Authorization header.

import type {
  LoginResponse,
  User,
  ProjectsResponse,
  ProjectDetailResponse,
  OperationsResponse,
  Operation,
  DatabasesResponse,
  CreateDatabaseResponse,
} from "./types";

// Empty string → relative URLs → requests go through the ingress proxy.
// Override with NEXT_PUBLIC_API_URL at build time only if needed (e.g. local dev).
const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "";

function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("dada_token");
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  token?: string;
};

export async function apiFetch<T>(
  path: string,
  options: RequestOptions = {}
): Promise<T> {
  const { method = "GET", body, token } = options;

  const bearerToken = token ?? getToken();

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  if (bearerToken) {
    headers["Authorization"] = `Bearer ${bearerToken}`;
  }

  const res = await fetch(`${API_BASE_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error((err as { error: string }).error ?? "API error");
  }

  return res.json() as Promise<T>;
}

// Convenience helpers
export const api = {
  get: <T>(path: string, token?: string) =>
    apiFetch<T>(path, { method: "GET", token }),

  post: <T>(path: string, body: unknown, token?: string) =>
    apiFetch<T>(path, { method: "POST", body, token }),

  put: <T>(path: string, body: unknown, token?: string) =>
    apiFetch<T>(path, { method: "PUT", body, token }),

  delete: <T>(path: string, token?: string) =>
    apiFetch<T>(path, { method: "DELETE", token }),
};

// Typed API functions
export const authApi = {
  login: (username: string, password: string) =>
    apiFetch<LoginResponse>("/api/v1/auth/login", { method: "POST", body: { username, password } }),
  me: () => apiFetch<{ user: User }>("/api/v1/auth/me"),
};

export const projectsApi = {
  list: () => apiFetch<ProjectsResponse>("/api/v1/projects"),
  get: (id: string) => apiFetch<ProjectDetailResponse>(`/api/v1/projects/${id}`),
  operations: (projectId: string) => apiFetch<OperationsResponse>(`/api/v1/projects/${projectId}/operations`),
  getOperation: (projectId: string, opId: string) =>
    apiFetch<{ operation: Operation }>(`/api/v1/projects/${projectId}/operations/${opId}`),
};

export const databasesApi = {
  list: (projectId: string, envId: string) =>
    apiFetch<DatabasesResponse>(`/api/v1/projects/${projectId}/environments/${envId}/databases`),
  create: (projectId: string, envId: string, data: {
    name: string; database: string; app_ref: string;
    backup_enabled: boolean; backup_schedule: string; backup_retention: string;
  }) =>
    apiFetch<CreateDatabaseResponse>(`/api/v1/projects/${projectId}/environments/${envId}/databases`, {
      method: "POST", body: data,
    }),
};
