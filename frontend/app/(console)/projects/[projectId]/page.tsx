// Project overview placeholder — full implementation in Task 4.
export default async function ProjectOverviewPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900">Project Overview</h1>
      <p className="mt-2 text-gray-500">Project ID: {projectId}</p>
    </div>
  );
}
