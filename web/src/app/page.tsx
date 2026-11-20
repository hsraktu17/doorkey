import { Suspense } from "react";
import { Dashboard } from "@/components/Dashboard";
import { DEFAULT_WINDOW, isWindow, type Window } from "@/lib/metrics";

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

export default function Page({ searchParams }: { searchParams: SearchParams }) {
  return (
    <Suspense fallback={<p className="py-16 text-center text-sm text-muted">Loading dashboard…</p>}>
      <DashboardFromParams searchParams={searchParams} />
    </Suspense>
  );
}

async function DashboardFromParams({ searchParams }: { searchParams: SearchParams }) {
  const raw = (await searchParams).window;
  const initial: Window = typeof raw === "string" && isWindow(raw) ? raw : DEFAULT_WINDOW;
  return <Dashboard initialWindow={initial} />;
}
