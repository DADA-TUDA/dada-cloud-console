import { redirect } from "next/navigation";

// Root page: redirect to projects (middleware will handle auth redirect to /login).
export default function RootPage() {
  redirect("/projects");
}
