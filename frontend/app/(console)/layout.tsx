// Console layout with sidebar placeholder — full implementation in Task 4.
import Link from "next/link";

export default function ConsoleLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="flex h-screen overflow-hidden bg-gray-100">
      {/* Sidebar placeholder */}
      <aside className="w-64 shrink-0 bg-gray-900 text-white">
        <div className="p-4">
          <h2 className="text-lg font-semibold">DADA Cloud</h2>
          <nav className="mt-6 space-y-1">
            <Link href="/projects" className="block rounded px-3 py-2 text-sm hover:bg-gray-700">
              Projects
            </Link>
          </nav>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto p-6">{children}</main>
    </div>
  );
}
