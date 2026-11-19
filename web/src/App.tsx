import { useEffect, useState } from "react";

type Health = { status: string; version: string };

type State =
  | { kind: "loading" }
  | { kind: "ok"; health: Health }
  | { kind: "error"; message: string };

export function App() {
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    const ctrl = new AbortController();
    fetch("/api/health", { signal: ctrl.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<Health>;
      })
      .then((health) => setState({ kind: "ok", health }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        setState({ kind: "error", message: err instanceof Error ? err.message : String(err) });
      });
    return () => ctrl.abort();
  }, []);

  return (
    <main style={{ fontFamily: "system-ui, sans-serif", padding: "2rem" }}>
      <h1>Ekumen</h1>
      <p data-testid="health">
        API health:{" "}
        {state.kind === "loading" && "checking…"}
        {state.kind === "ok" && `${state.health.status} (version ${state.health.version})`}
        {state.kind === "error" && `unreachable (${state.message})`}
      </p>
    </main>
  );
}
