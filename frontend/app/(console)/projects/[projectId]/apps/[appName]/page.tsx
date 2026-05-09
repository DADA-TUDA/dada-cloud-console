"use client";
import { useEffect, useState, FormEvent } from "react";
import { useParams, useSearchParams, useRouter } from "next/navigation";
import Link from "next/link";
import { appsApi } from "@/lib/api";
import type { ResourceSnapshot, AppSummary } from "@/lib/types";
import { Modal } from "@/components/ui/modal";
import { Spinner } from "@/components/ui/spinner";

function PhaseBadge({ phase }: { phase?: string }) {
  const p = phase ?? "";
  const isReady = p.toLowerCase() === "ready";
  return (
    <span className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
      isReady ? "bg-green-100 text-green-700" : "bg-yellow-100 text-yellow-700"
    }`}>
      {p || "Unknown"}
    </span>
  );
}

export default function AppDetailPage() {
  const params = useParams<{ projectId: string; appName: string }>();
  const searchParams = useSearchParams();
  const router = useRouter();
  const { projectId, appName } = params;
  const envId = searchParams.get("envId") ?? "";

  const [app, setApp] = useState<ResourceSnapshot | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [newImage, setNewImage] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (!envId) { setIsLoading(false); setError("Missing environment ID"); return; }
    appsApi.list(projectId, envId)
      .then((data) => {
        const found = (data.apps ?? []).find((a) => a.name === appName);
        if (!found) setError("App not found");
        else setApp(found);
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Failed to load app"))
      .finally(() => setIsLoading(false));
  }, [projectId, appName, envId]);

  async function handleImageUpdate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSubmitError(null);
    setIsSubmitting(true);
    try {
      const result = await appsApi.updateImage(projectId, envId, appName, newImage);
      setIsModalOpen(false);
      setNewImage("");
      const opId = result.operation?.id;
      setTimeout(() => {
        router.push(`/projects/${projectId}/operations${opId ? `?highlight=${opId}` : ""}`);
      }, 1500);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to update image");
    } finally {
      setIsSubmitting(false);
    }
  }

  if (isLoading) return <div className="flex h-64 items-center justify-center"><Spinner size="lg" /></div>;
  if (error || !app) {
    return <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error ?? "App not found"}</div>;
  }

  const summary = app.summary_json as unknown as AppSummary;

  return (
    <div>
      <div className="mb-8 flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <Link href="/projects" className="hover:text-gray-700">Projects</Link>
            <span>/</span>
            <Link href={`/projects/${projectId}`} className="hover:text-gray-700">Overview</Link>
            <span>/</span>
            <Link href={`/projects/${projectId}/apps`} className="hover:text-gray-700">Applications</Link>
            <span>/</span>
            <span className="text-gray-900 font-mono">{appName}</span>
          </div>
          <div className="mt-2 flex items-center gap-3">
            <h1 className="text-2xl font-bold text-gray-900 font-mono">{appName}</h1>
            <PhaseBadge phase={app.phase} />
          </div>
        </div>
        <button
          onClick={() => { setNewImage(summary.image ?? ""); setIsModalOpen(true); }}
          className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors"
        >
          <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
          </svg>
          Deploy Image
        </button>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {([
          { label: "Image", value: summary.image ?? "—", mono: true },
          { label: "Profile", value: summary.profile ?? "small" },
          { label: "Replicas", value: String(summary.replicas ?? 2) },
          { label: "Port", value: String(summary.port ?? 8080) },
        ] as { label: string; value: string; mono?: boolean }[]).map(({ label, value, mono }) => (
          <div key={label} className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
            <p className="text-xs font-semibold uppercase tracking-wide text-gray-400">{label}</p>
            <p className={`mt-1 text-sm font-medium text-gray-900 truncate ${mono ? "font-mono" : ""}`}>{value}</p>
          </div>
        ))}
      </div>

      <Modal
        isOpen={isModalOpen}
        onClose={() => { setIsModalOpen(false); setSubmitError(null); }}
        title="Deploy New Image"
      >
        <form onSubmit={handleImageUpdate} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">New Image Tag</label>
            <input
              type="text"
              required
              value={newImage}
              onChange={(e) => setNewImage(e.target.value)}
              placeholder="ghcr.io/org/service:v2.0.0"
              className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-mono text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
            <p className="mt-1 text-xs text-gray-400">Current: <span className="font-mono">{summary.image ?? "—"}</span></p>
          </div>

          {submitError && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{submitError}</div>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <button type="button" onClick={() => { setIsModalOpen(false); setSubmitError(null); }}
              className="rounded-lg px-4 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100 transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={isSubmitting}
              className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50 transition-colors">
              {isSubmitting ? <><Spinner size="sm" /> Deploying...</> : "Deploy"}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
