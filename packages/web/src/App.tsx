import { useCallback, useEffect, useState } from "react";
import { api } from "./api";
import { AppShell } from "./components/AppShell";
import { CaptureForm } from "./components/CaptureForm";
import { DoctorPanel } from "./components/DoctorPanel";
import { ThreadList } from "./components/ThreadList";
import { ThreadWorkspace } from "./components/ThreadWorkspace";
import type { AppInfo, ThreadSummary } from "./types";

type Route =
  | { page: "threads" | "doctor" | "capture" }
  | { page: "thread"; id: string };

function readRoute(): Route {
  const hash = window.location.hash.replace(/^#\/?/, "");
  if (hash.startsWith("threads/"))
    return { page: "thread", id: hash.slice("threads/".length) };
  if (hash === "doctor") return { page: "doctor" };
  if (hash === "capture") return { page: "capture" };
  return { page: "threads" };
}

function setLocation(route: Route) {
  const hash =
    route.page === "thread"
      ? `threads/${route.id}`
      : route.page === "threads"
        ? ""
        : route.page;
  window.location.hash = `#/${hash}`;
}

export function App() {
  const [route, setRoute] = useState<Route>(() => readRoute());
  const [info, setInfo] = useState<AppInfo>();
  const [threads, setThreads] = useState<ThreadSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [fatal, setFatal] = useState("");

  const load = useCallback(async () => {
    try {
      const [nextInfo, nextThreads] = await Promise.all([
        api.info(),
        api.threads(),
      ]);
      setInfo(nextInfo);
      setThreads(nextThreads);
      setFatal("");
    } catch (reason) {
      setFatal(
        reason instanceof Error
          ? reason.message
          : "Unable to reach Team Cross Core",
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    const listener = () => setRoute(readRoute());
    window.addEventListener("hashchange", listener);
    return () => window.removeEventListener("hashchange", listener);
  }, []);

  function navigate(next: Route) {
    setLocation(next);
    setRoute(next);
  }

  if (fatal && !info) {
    return (
      <main className="fatal-screen">
        <span className="fatal-logo">×</span>
        <h1>Team Cross Core is unavailable</h1>
        <p>{fatal}</p>
        <button className="button primary" onClick={load} type="button">
          Retry connection
        </button>
      </main>
    );
  }

  const shellPage = route.page === "doctor" ? "doctor" : "threads";
  return (
    <AppShell
      active={shellPage}
      info={info}
      onNavigate={(page) => navigate({ page })}
      onNewThread={() => navigate({ page: "capture" })}
    >
      {route.page === "threads" ? (
        <ThreadList
          canCreate={info?.role === "owner"}
          loading={loading}
          onNew={() => navigate({ page: "capture" })}
          onOpen={(id) => navigate({ page: "thread", id })}
          threads={threads}
        />
      ) : null}
      {route.page === "doctor" ? <DoctorPanel info={info} /> : null}
      {route.page === "capture" ? (
        <CaptureForm
          defaultRepo={info?.repo}
          onCancel={() => navigate({ page: "threads" })}
          onCreated={(id) => {
            void load();
            navigate({ page: "thread", id });
          }}
        />
      ) : null}
      {route.page === "thread" && info ? (
        <ThreadWorkspace
          id={route.id}
          info={info}
          onBack={() => {
            void load();
            navigate({ page: "threads" });
          }}
        />
      ) : null}
    </AppShell>
  );
}
