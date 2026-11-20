"use client";

import { WINDOWS, type Window } from "@/lib/metrics";

export function WindowPicker({ value, onChange }: { value: Window; onChange: (w: Window) => void }) {
  return (
    <div role="radiogroup" aria-label="Time window" className="inline-flex rounded-lg border border-line bg-surface p-0.5">
      {WINDOWS.map((w) => {
        const active = w.value === value;
        return (
          <button
            key={w.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(w.value)}
            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
              active ? "bg-ink text-page" : "text-ink-2 hover:bg-hover hover:text-ink"
            }`}
          >
            {w.label}
          </button>
        );
      })}
    </div>
  );
}
