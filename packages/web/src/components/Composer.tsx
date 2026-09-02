import { useState } from "react";
import type { AgentRun, PendingInput, Role } from "../types";
import { AgentIcon, StopIcon } from "./Icons";

function InputRequestComposer({
  pendingInput,
  role,
  onRespondInput,
  onInterrupt,
  onRequestControl,
}: {
  pendingInput: PendingInput;
  role: Role;
  onRespondInput: (inputRequestId: string, response: string) => Promise<void>;
  onInterrupt: () => Promise<void>;
  onRequestControl: () => Promise<void>;
}) {
  const [response, setResponse] = useState("");
  const [busy, setBusy] = useState(false);
  const canControl = role === "owner" || role === "controller";

  async function submit() {
    const value = response.trim();
    if (!value) return;
    setBusy(true);
    try {
      await onRespondInput(pendingInput.id, value);
      setResponse("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section
      aria-label="Agent input request"
      className="input-request-composer"
    >
      <header>
        <span>Agent needs input</span>
        {pendingInput.blocking ? <em>Turn paused</em> : null}
      </header>
      <div className="input-question-list">
        {pendingInput.questions.map((question, index) => (
          <div key={question.id ?? `${index}-${question.question}`}>
            {question.header ? <small>{question.header}</small> : null}
            <strong>{question.question}</strong>
            {question.options?.length ? (
              <ul>
                {question.options.map((option) => (
                  <li key={option.label}>
                    {option.label}
                    {option.description ? ` — ${option.description}` : ""}
                  </li>
                ))}
              </ul>
            ) : null}
          </div>
        ))}
      </div>
      {canControl ? (
        <>
          <textarea
            aria-label="Response to Agent input"
            disabled={busy}
            onChange={(event) => setResponse(event.target.value)}
            value={response}
          />
          <footer>
            <button
              className="button ghost compact"
              onClick={onInterrupt}
              type="button"
            >
              <StopIcon size={14} /> Interrupt
            </button>
            <button
              className="button primary compact"
              disabled={busy || !response.trim()}
              onClick={() => void submit()}
              type="button"
            >
              {busy ? "Responding…" : "Respond"}
            </button>
          </footer>
        </>
      ) : (
        <footer>
          <span>Request control to answer this prompt.</span>
          <button
            className="button secondary compact"
            onClick={onRequestControl}
            type="button"
          >
            Request control
          </button>
        </footer>
      )}
    </section>
  );
}

export function Composer({
  role,
  run,
  pendingInput,
  onSend,
  onSteer,
  onInterrupt,
  onRespondInput,
  onRequestControl,
}: {
  role: Role;
  run?: AgentRun;
  pendingInput?: PendingInput;
  onSend: (text: string) => Promise<void>;
  onSteer: (text: string) => Promise<void>;
  onInterrupt: () => Promise<void>;
  onRespondInput: (inputRequestId: string, response: string) => Promise<void>;
  onRequestControl: () => Promise<void>;
}) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const canControl = role === "owner" || role === "controller";
  const running = run?.status === "running";

  if (pendingInput) {
    return (
      <InputRequestComposer
        key={pendingInput.id}
        onInterrupt={onInterrupt}
        onRequestControl={onRequestControl}
        onRespondInput={onRespondInput}
        pendingInput={pendingInput}
        role={role}
      />
    );
  }

  async function submit(mode: "send" | "steer") {
    const value = text.trim();
    if (!value) return;
    setBusy(true);
    try {
      await (mode === "steer" ? onSteer(value) : onSend(value));
      setText("");
    } finally {
      setBusy(false);
    }
  }

  if (!canControl) {
    return (
      <div className="observer-composer">
        <span>Observer mode</span>
        <p>
          Request the single 60-second control lease to send instructions to the
          host Agent.
        </p>
        <button
          className="button secondary"
          onClick={onRequestControl}
          type="button"
        >
          Request control
        </button>
      </div>
    );
  }

  return (
    <div className="composer">
      <textarea
        aria-label="Message to Agent"
        disabled={!run || busy}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            void submit(running ? "steer" : "send");
          }
        }}
        placeholder={
          run
            ? running
              ? "Add direction to the active turn…"
              : `Message ${run.provider}…`
            : "Choose an Agent to begin"
        }
        value={text}
      />
      <div className="composer-footer">
        <span>
          {run ? (
            <>
              <AgentIcon size={14} /> {run.provider} · network{" "}
              {run.networkEnabled ? "on" : "off"}
            </>
          ) : (
            "No managed session"
          )}
        </span>
        <div>
          {running ? (
            <button
              className="button ghost compact"
              onClick={onInterrupt}
              type="button"
            >
              <StopIcon size={14} /> Interrupt
            </button>
          ) : null}
          <button
            className="button primary compact"
            disabled={!run || busy || !text.trim()}
            onClick={() => submit(running ? "steer" : "send")}
            type="button"
          >
            {running ? "Steer" : "Send"}
          </button>
        </div>
      </div>
    </div>
  );
}
