"use client";
import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "next/navigation";
import Link from "next/link";
import { projectsApi } from "@/lib/api";
import type { Operation, OperationStatus } from "@/lib/types";
import { Spinner } from "@/components/ui/spinner";

const IN_PROGRESS_STATUSES = new Set<OperationStatus>([
  "Created", "Validated", "Queued", "Rendering",
  "CommittingToGit", "Committed", "WaitingForArgoSync",
  "Syncing", "Reconciling", "WaitingForApproval",
]);

function isInProgress(status: OperationStatus): boolean {
  return IN_PROGRESS_STATUSES.has(status);
}

function statusColor(status: OperationStatus): string {
  if (status === "Ready") return "bg-green-100 text-green-700";
  if (status === "Failed") return "bg-red-100 text-red-700";
  if (status === "Cancelled") return "bg-gray-100 text-gray-600";
  if (status === "WaitingForApproval") return "bg-yellow-100 text-yellow-700";
  return "bg-blue-100 text-blue-700";
}

function statusDot(status: OperationStatus): string {
  if (status === "Ready") return "bg-green-500";
  if (status === "Failed") return "bg-red-500";
  if (status === "Cancelled") return "bg-gray-400";
  if (status === "WaitingForApproval") return "bg-yellow-500";
  return "bg-blue-500";
}

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSecs = Math.floor(diffMs / 1000);
  if (diffSecs < 60) return `${diffSecs}s ago`;
  const diffMins = Math.floor(diffSecs / 60);
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays}d ago`;
}

function StatusIcon({ status }: { status: OperationStatus }) {
  if (status === "Ready") {
    return (
      <svg className="h-4 w-4 text-green-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
      </svg>
    );
  }
  if (status === "Failed") {
    return (
      <svg className="h-4 w-4 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
      </svg>
    );
  }
  if (status === "Cancelled") {
    return (
      <svg className="h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
      </svg>
    );
  }
  if (status === "WaitingForApproval") {
    return (
      <svg className="h-4 w-4 text-yellow-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    );
  }
  // In-progress: spinning dots
  return (
    <svg className="h-4 w-4 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
    </svg>
  );
}

export default function OperationsPage() {
  const params = useParams<{ projectId: string }>();
  const searchParams = useSearchParams();
  const projectId = params.projectId;
  const highlightId = searchParams.get("highlight");

  const [operations, setOperations] = useState<Operation[]>([]);
  const [loadedProjectId, setLoadedProjectId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(highlightId);

  useEffect(() => {
    let cancelled = false;
    void projectsApi
      .operations(projectId)
      .then((data) => {
        if (cancelled) return;
        setOperations(data.operations ?? []);
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load operations");
      })
      .finally(() => {
        if (!cancelled) {
          setLoadedProjectId(projectId);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  // Auto-refresh if any operation is in-progress
  useEffect(() => {
    const hasInProgress = operations.some((op) => isInProgress(op.status));
    if (!hasInProgress) return;
    const interval = setInterval(() => {
      void projectsApi.operations(projectId).then((data) => {
        setOperations(data.operations ?? []);
        setError(null);
      });
    }, 3000);
    return () => clearInterval(interval);
  }, [operations, projectId]);

  if (loadedProjectId !== projectId) {
    return (
      <div className="flex h-64 items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const hasInProgress = operations.some((op) => isInProgress(op.status));

  return (
    <div>
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-2 text-sm text-gray-500">
          <Link href="/projects" className="hover:text-gray-700">Projects</Link>
          <span>/</span>
          <Link href={`/projects/${projectId}`} className="hover:text-gray-700">Overview</Link>
          <span>/</span>
          <span className="text-gray-900">Operations</span>
        </div>
        <div className="mt-2 flex items-center gap-3">
          <h1 className="text-2xl font-bold text-gray-900">Operations</h1>
          {hasInProgress && (
            <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-50 px-2.5 py-1 text-xs font-medium text-blue-600">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-600" />
              Live
            </span>
          )}
        </div>
        <p className="mt-0.5 text-sm text-gray-500">Deployment and provisioning history</p>
      </div>

      {error && (
        <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      )}

      {operations.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-gray-300 bg-gray-50 py-16">
          <svg className="mb-3 h-12 w-12 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
          </svg>
          <p className="text-sm font-medium text-gray-500">No operations yet</p>
          <p className="mt-1 text-xs text-gray-400">Operations appear here when you create or modify resources.</p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-gray-200 bg-white">
          {operations.map((op, idx) => {
            const isExpanded = expandedId === op.id;
            const isHighlighted = highlightId === op.id;
            return (
              <div
                key={op.id}
                className={`${idx < operations.length - 1 ? "border-b border-gray-100" : ""} ${
                  isHighlighted ? "bg-blue-50/50" : ""
                }`}
              >
                {/* Row */}
                <button
                  onClick={() => setExpandedId(isExpanded ? null : op.id)}
                  className="w-full px-5 py-4 text-left hover:bg-gray-50 transition-colors"
                >
                  <div className="flex items-center gap-4">
                    {/* Status icon */}
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-gray-100 bg-white shadow-sm">
                      <StatusIcon status={op.status} />
                    </div>

                    {/* Main info */}
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium text-gray-900">{op.action}</span>
                        <span className="font-mono text-xs text-gray-500">{op.resource_name}</span>
                        {op.resource_kind && (
                          <span className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-500">
                            {op.resource_kind}
                          </span>
                        )}
                      </div>
                      <div className="mt-1 flex items-center gap-3 text-xs text-gray-400">
                        <span>{timeAgo(op.created_at)}</span>
                        {op.git_commit && (
                          <>
                            <span>·</span>
                            <span className="font-mono">{op.git_commit.slice(0, 8)}</span>
                          </>
                        )}
                      </div>
                    </div>

                    {/* Status badge */}
                    <div className="flex items-center gap-2">
                      <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${statusColor(op.status)}`}>
                        <span className={`h-1.5 w-1.5 rounded-full ${statusDot(op.status)}`} />
                        {op.status}
                      </span>
                      <svg
                        className={`h-4 w-4 text-gray-400 transition-transform ${isExpanded ? "rotate-180" : ""}`}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                      </svg>
                    </div>
                  </div>
                </button>

                {/* Expanded detail */}
                {isExpanded && (
                  <div className="border-t border-gray-100 bg-gray-50 px-5 py-4">
                    <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
                      <div>
                        <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Operation ID</dt>
                        <dd className="mt-1 font-mono text-xs text-gray-700">{op.id}</dd>
                      </div>
                      <div>
                        <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Action</dt>
                        <dd className="mt-1 text-xs text-gray-700">{op.action}</dd>
                      </div>
                      <div>
                        <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Resource</dt>
                        <dd className="mt-1 font-mono text-xs text-gray-700">{op.resource_name}</dd>
                      </div>
                      {op.git_commit && (
                        <div>
                          <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Git Commit</dt>
                          <dd className="mt-1 font-mono text-xs text-gray-700">{op.git_commit}</dd>
                        </div>
                      )}
                      {op.git_path && (
                        <div>
                          <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Git Path</dt>
                          <dd className="mt-1 font-mono text-xs text-gray-700 break-all">{op.git_path}</dd>
                        </div>
                      )}
                      <div>
                        <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Created</dt>
                        <dd className="mt-1 text-xs text-gray-700">{new Date(op.created_at).toLocaleString()}</dd>
                      </div>
                      <div>
                        <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">Updated</dt>
                        <dd className="mt-1 text-xs text-gray-700">{new Date(op.updated_at).toLocaleString()}</dd>
                      </div>
                    </dl>
                    {op.error_message && (
                      <div className="mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2">
                        <p className="text-xs font-medium text-red-700">Error</p>
                        <p className="mt-0.5 font-mono text-xs text-red-600">{op.error_message}</p>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
