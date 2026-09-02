import { useEffect, useState } from "react";
import type { CapturePreview } from "../types";
import { api } from "../api";
import { GitIcon } from "./Icons";

interface CaptureFormProps {
  defaultRepo?: string;
  onCancel: () => void;
  onCreated: (threadId: string) => void;
}

const blankPreview: CapturePreview = {
  repo: "",
  branch: "",
  head: "",
  unborn: false,
  status: "",
  untracked: [],
};

export function CaptureForm({
  defaultRepo = ".",
  onCancel,
  onCreated,
}: CaptureFormProps) {
  const [repo, setRepo] = useState(defaultRepo);
  const [preview, setPreview] = useState<CapturePreview>(blankPreview);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [fields, setFields] = useState({
    title: "",
    goal: "",
    progress: "",
    blocker: "",
    tried: "",
    questions: "",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(() => {
      api
        .preview(repo)
        .then((result) => {
          if (cancelled) return;
          setPreview(result);
          setSelected(new Set(result.untracked.map((file) => file.path)));
          setError("");
        })
        .catch((reason: unknown) => {
          if (!cancelled)
            setError(
              reason instanceof Error
                ? reason.message
                : "Unable to inspect repository",
            );
        });
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [repo]);

  function updateField(name: keyof typeof fields, value: string) {
    setFields((current) => ({ ...current, [name]: value }));
  }

  function updateRepo(value: string) {
    setRepo(value);
    setPreview(blankPreview);
    setSelected(new Set());
    setError("");
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const thread = await api.createThread({
        repo,
        ...fields,
        untracked: [...selected],
      });
      onCreated(thread.id);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Capture failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="page page-capture">
      <header className="page-header">
        <div>
          <p className="eyebrow">New thread</p>
          <h1>Capture development context</h1>
          <p>This creates an immutable baseline and an isolated worktree.</p>
        </div>
      </header>
      <form className="capture-layout" onSubmit={submit}>
        <div className="capture-form surface">
          <label className="field">
            <span>Repository</span>
            <input
              required
              value={repo}
              onChange={(event) => updateRepo(event.target.value)}
            />
          </label>
          <div className="git-preview">
            <GitIcon />
            <div>
              <strong>
                {preview.branch ||
                  (preview.unborn ? "Unborn repository" : "Inspecting…")}
              </strong>
              <small>
                {preview.head
                  ? preview.head.slice(0, 12)
                  : "No baseline commit"}
              </small>
            </div>
            <span className="change-count">
              {preview.status
                ? preview.status.split("\n").filter(Boolean).length
                : 0}{" "}
              changes
            </span>
          </div>
          <label className="field">
            <span>Thread title</span>
            <input
              required
              placeholder="Fix checkout race on reconnect"
              value={fields.title}
              onChange={(event) => updateField("title", event.target.value)}
            />
          </label>
          <label className="field">
            <span>Goal</span>
            <textarea
              required
              placeholder="What outcome are you trying to reach?"
              value={fields.goal}
              onChange={(event) => updateField("goal", event.target.value)}
            />
          </label>
          <div className="field-pair">
            <label className="field">
              <span>Current progress</span>
              <textarea
                placeholder="What is already working?"
                value={fields.progress}
                onChange={(event) =>
                  updateField("progress", event.target.value)
                }
              />
            </label>
            <label className="field">
              <span>Blocker</span>
              <textarea
                placeholder="Where are you stuck?"
                value={fields.blocker}
                onChange={(event) => updateField("blocker", event.target.value)}
              />
            </label>
          </div>
          <label className="field">
            <span>Already tried</span>
            <textarea
              placeholder="Commands, approaches, and results"
              value={fields.tried}
              onChange={(event) => updateField("tried", event.target.value)}
            />
          </label>
          <label className="field">
            <span>Question for collaborator</span>
            <textarea
              placeholder="What decision or investigation would unblock you?"
              value={fields.questions}
              onChange={(event) => updateField("questions", event.target.value)}
            />
          </label>
          {error ? (
            <p className="form-error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="form-actions">
            <button className="button ghost" onClick={onCancel} type="button">
              Cancel
            </button>
            <button
              className="button primary"
              disabled={busy || !preview.repo}
              type="submit"
            >
              {busy ? "Capturing…" : "Capture & create"}
            </button>
          </div>
        </div>
        <aside className="capture-sidebar surface">
          <div>
            <p className="eyebrow">Included files</p>
            <h2>Untracked files</h2>
            <p>
              Selected files are copied into the isolated worktree. Limits: 5
              MiB each, 20 MiB total.
            </p>
          </div>
          <div className="untracked-list">
            {preview.untracked.map((file) => (
              <label key={file.path}>
                <input
                  checked={selected.has(file.path)}
                  onChange={(event) =>
                    setSelected((current) => {
                      const next = new Set(current);
                      if (event.target.checked) next.add(file.path);
                      else next.delete(file.path);
                      return next;
                    })
                  }
                  type="checkbox"
                />
                <span>
                  <strong>{file.path}</strong>
                  <small>{Math.max(1, Math.ceil(file.size / 1024))} KiB</small>
                </span>
              </label>
            ))}
            {preview.untracked.length === 0 ? (
              <p className="empty-inline">No untracked files found.</p>
            ) : null}
          </div>
          <div className="capture-safety">
            <strong>Original workspace stays untouched</strong>
            <p>Agent edits happen only in a Team Cross worktree.</p>
          </div>
        </aside>
      </form>
    </section>
  );
}
