import * as React from "react";
import { clsx } from "clsx";
import type { OperationStatus } from "@/lib/types";

const statusColorMap: Record<string, string> = {
  Created: "bg-gray-100 text-gray-800",
  Validated: "bg-blue-100 text-blue-800",
  Queued: "bg-yellow-100 text-yellow-800",
  Rendering: "bg-purple-100 text-purple-800",
  CommittingToGit: "bg-indigo-100 text-indigo-800",
  Committed: "bg-indigo-200 text-indigo-900",
  WaitingForArgoSync: "bg-orange-100 text-orange-800",
  Syncing: "bg-cyan-100 text-cyan-800",
  Reconciling: "bg-teal-100 text-teal-800",
  Ready: "bg-green-100 text-green-800",
  Failed: "bg-red-100 text-red-800",
  Cancelled: "bg-gray-200 text-gray-600",
  WaitingForApproval: "bg-amber-100 text-amber-800",
};

interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  status?: OperationStatus | string;
}

export function Badge({ status, className, children, ...props }: BadgeProps) {
  const colorClass = status ? (statusColorMap[status] ?? "bg-gray-100 text-gray-800") : "";

  return (
    <span
      className={clsx(
        "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium",
        colorClass,
        className
      )}
      {...props}
    >
      {children ?? status}
    </span>
  );
}
