import type { ReactNode } from "react";
import type { StatusTone } from "@/lib/format";

export function Card({
  title,
  subtitle,
  action,
  children,
  className = "",
}: {
  title: string;
  subtitle?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const id = `card-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return (
    <section
      aria-labelledby={id}
      className={`min-w-0 rounded-xl border border-line bg-surface p-4 sm:p-5 ${className}`}
    >
      <header className="mb-3 flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 id={id} className="text-sm font-semibold text-ink">
            {title}
          </h2>
          {subtitle ? <p className="mt-0.5 text-xs text-ink-2">{subtitle}</p> : null}
        </div>
        {action}
      </header>
      {children}
    </section>
  );
}

export const toneVar: Record<StatusTone, string> = {
  good: "var(--status-good)",
  warning: "var(--status-warning)",
  serious: "var(--status-serious)",
  critical: "var(--status-critical)",
  neutral: "var(--status-neutral)",
};

/** A status number with a colored dot; the number itself carries the meaning. */
export function StatusBadge({ status, tone }: { status: number; tone: StatusTone }) {
  return (
    <span className="inline-flex items-center gap-1.5 tabular text-ink">
      <span aria-hidden className="size-2 rounded-full" style={{ background: toneVar[tone] }} />
      {status === 0 ? "network" : status}
    </span>
  );
}

/** Legend entry: a swatch shaped like the mark (rect for bars, line for lines). */
export function LegendItem({ color, label, shape = "rect" }: { color: string; label: string; shape?: "rect" | "line" }) {
  return (
    <li className="flex items-center gap-1.5 text-xs text-ink-2">
      {shape === "rect" ? (
        <span aria-hidden className="size-2.5 rounded-[3px]" style={{ background: color }} />
      ) : (
        <span aria-hidden className="h-0.5 w-3.5 rounded-full" style={{ background: color }} />
      )}
      {label}
    </li>
  );
}

export function Legend({ children }: { children: ReactNode }) {
  return <ul className="flex flex-wrap items-center gap-x-4 gap-y-1">{children}</ul>;
}

/** Collapsible table view so every charted value is readable without hovering. */
export function DataTable({ summary, children }: { summary: string; children: ReactNode }) {
  return (
    <details className="mt-3 text-xs text-ink-2">
      <summary className="cursor-pointer select-none hover:text-ink">{summary}</summary>
      <div className="mt-2 max-h-64 overflow-auto">{children}</div>
    </details>
  );
}

export const th = "px-2 py-1.5 text-left font-medium text-muted";
export const td = "whitespace-nowrap px-2 py-1.5 tabular text-ink";
