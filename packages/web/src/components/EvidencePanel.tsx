import { useState } from "react";
import { api } from "../api";
import type { Evidence } from "../types";

type EvidenceKind = "text" | "log" | "file";

export interface EvidenceAttachment {
  kind: EvidenceKind;
  name: string;
  source: string;
  contentBase64: string;
  mimeType: string;
}

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () =>
      reject(reader.error ?? new Error("Unable to read evidence"));
    reader.onload = () => {
      const value = typeof reader.result === "string" ? reader.result : "";
      const separator = value.indexOf(",");
      if (separator < 0) {
        reject(new Error("Unable to encode evidence"));
        return;
      }
      resolve(value.slice(separator + 1));
    };
    reader.readAsDataURL(blob);
  });
}

export function EvidencePanel({
  threadId,
  evidence,
  canManage,
  onAttach,
  onAnnotate,
}: {
  threadId: string;
  evidence: Evidence[];
  canManage: boolean;
  onAttach: (input: EvidenceAttachment) => Promise<void>;
  onAnnotate?: (evidenceId: string) => void;
}) {
  const [kind, setKind] = useState<EvidenceKind>("text");
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [file, setFile] = useState<File>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const ready = kind === "file" ? Boolean(file) : Boolean(content.trim());

  async function attach() {
    if (!ready) return;
    setBusy(true);
    setError("");
    try {
      const blob =
        kind === "file"
          ? file!
          : new Blob([content], { type: "text/plain;charset=utf-8" });
      await onAttach({
        kind,
        name:
          kind === "file"
            ? file!.name
            : name.trim() || (kind === "log" ? "attached.log" : "note.txt"),
        source: "webgui",
        contentBase64: await blobToBase64(blob),
        mimeType:
          kind === "file"
            ? file!.type || "application/octet-stream"
            : "text/plain;charset=utf-8",
      });
      setName("");
      setContent("");
      setFile(undefined);
    } catch (reason) {
      setError(
        reason instanceof Error ? reason.message : "Unable to attach evidence",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="evidence-view">
      {canManage ? (
        <section
          className="evidence-compose surface"
          aria-label="Attach evidence"
        >
          <div className="evidence-kind segmented" aria-label="Evidence type">
            {(["text", "log", "file"] as const).map((value) => (
              <button
                aria-pressed={kind === value}
                className={kind === value ? "active" : ""}
                key={value}
                onClick={() => {
                  setKind(value);
                  setError("");
                }}
                type="button"
              >
                {value === "file" ? "Local file" : value}
              </button>
            ))}
          </div>
          {kind === "file" ? (
            <label className="file-evidence-input">
              <span>Choose a local file</span>
              <input
                aria-label="Evidence file"
                onChange={(event) => setFile(event.target.files?.[0])}
                type="file"
              />
              <small>{file?.name ?? "No file selected"}</small>
            </label>
          ) : (
            <>
              <label>
                Name
                <input
                  aria-label="Evidence name"
                  onChange={(event) => setName(event.target.value)}
                  placeholder={
                    kind === "log" ? "build.log" : "investigation note"
                  }
                  value={name}
                />
              </label>
              <label>
                {kind === "log" ? "Log content" : "Text content"}
                <textarea
                  aria-label="Evidence content"
                  onChange={(event) => setContent(event.target.value)}
                  placeholder="Paste context that should travel with this thread…"
                  value={content}
                />
              </label>
            </>
          )}
          <div className="evidence-compose-actions">
            {error ? <span role="alert">{error}</span> : <span />}
            <button
              className="button secondary compact"
              disabled={!ready || busy}
              onClick={() => void attach()}
              type="button"
            >
              {busy ? "Attaching…" : "Attach evidence"}
            </button>
          </div>
        </section>
      ) : null}

      <div className="evidence-grid">
        {evidence.map((item) => {
          const contentUrl = api.evidenceContent(threadId, item.id);
          const downloadUrl = api.evidenceContent(threadId, item.id, true);
          return (
            <article className="surface" key={item.id}>
              <span>{item.kind.slice(0, 1).toUpperCase()}</span>
              <div>
                <strong>{item.name}</strong>
                <small>
                  {item.kind} · {Math.ceil(item.size / 1024)} KiB
                </small>
              </div>
              <div className="evidence-actions">
                <a href={contentUrl} rel="noreferrer" target="_blank">
                  View
                </a>
                <a download={item.name} href={downloadUrl}>
                  Download
                </a>
                {onAnnotate ? (
                  <button
                    className="text-button"
                    onClick={() => onAnnotate(item.id)}
                    type="button"
                  >
                    批注
                  </button>
                ) : null}
              </div>
            </article>
          );
        })}
        {evidence.length === 0 ? (
          <div className="tab-empty">
            <h2>No extra evidence</h2>
            <p>
              {canManage
                ? "Attach a log, text note, or local file without changing the repository."
                : "The host has not attached any extra evidence."}
            </p>
          </div>
        ) : null}
      </div>
    </div>
  );
}
