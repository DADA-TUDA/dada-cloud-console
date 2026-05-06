// Database list placeholder — full implementation in Task 4.
export default async function DatabasesPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900">Databases</h1>
      <p className="mt-2 text-gray-500">
        ServiceDatabase resources for project {projectId} — coming in Task 4.
      </p>
    </div>
  );
}
