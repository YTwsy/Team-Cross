import type { Round, TimelineEvent } from "../types";
import { AgentIcon, GitIcon } from "./Icons";

function textValue(value: unknown): string {
  if (typeof value === "string") return value;
  return JSON.stringify(value);
}

function messageKey(event: TimelineEvent): string {
  const key = event.payload.messageId ?? event.payload.turnId;
  return typeof key === "string" && key ? key : `event-${event.seq}`;
}

function visibleEvents(events: TimelineEvent[]): TimelineEvent[] {
  const completed = new Set(
    events
      .filter((event) => event.type === "message.completed")
      .map(messageKey),
  );
  const live = new Map<
    string,
    { event: TimelineEvent; text: string; lastSeq: number }
  >();

  for (const event of events) {
    if (event.type !== "message.delta") continue;
    const key = messageKey(event);
    if (completed.has(key)) continue;
    const current = live.get(key);
    const delta = textValue(event.payload.delta ?? "");
    live.set(key, {
      event,
      text: `${current?.text ?? ""}${delta}`,
      lastSeq: event.seq,
    });
  }

  const liveBySequence = new Map(
    [...live.values()].map(({ event, text, lastSeq }) => [
      lastSeq,
      {
        ...event,
        seq: lastSeq,
        payload: { ...event.payload, text },
      },
    ]),
  );
  return events.flatMap((event) => {
    if (event.type !== "message.delta") return [event];
    const liveEvent = liveBySequence.get(event.seq);
    return liveEvent ? [liveEvent] : [];
  });
}

function EventCard({ event }: { event: TimelineEvent }) {
  if (event.type === "message.completed" || event.type === "message.delta") {
    const streaming = event.type === "message.delta";
    return (
      <article
        aria-live={streaming ? "polite" : undefined}
        className={`timeline-event message-event${streaming ? " streaming" : ""}`}
      >
        <div className="event-avatar agent">
          <AgentIcon size={15} />
        </div>
        <div>
          <header>
            <strong>{textValue(event.payload.provider ?? "Agent")}</strong>
            <time>
              {streaming
                ? "responding…"
                : new Date(event.createdAt).toLocaleTimeString([], {
                    hour: "2-digit",
                    minute: "2-digit",
                  })}
            </time>
          </header>
          <p>
            {textValue(event.payload.text ?? "")}
            {streaming ? (
              <i className="streaming-cursor" aria-hidden="true" />
            ) : null}
          </p>
        </div>
      </article>
    );
  }
  if (
    event.type === "command.send" ||
    event.type === "command.sent" ||
    event.type === "command.steer"
  ) {
    return (
      <article className="timeline-event message-event user-message">
        <div className="event-avatar user">
          {(event.actor ?? "Y").slice(0, 1).toUpperCase()}
        </div>
        <div>
          <header>
            <strong>{event.actor ?? "Owner"}</strong>
            <time>
              {new Date(event.createdAt).toLocaleTimeString([], {
                hour: "2-digit",
                minute: "2-digit",
              })}
            </time>
          </header>
          <p>{textValue(event.payload.text ?? "")}</p>
        </div>
      </article>
    );
  }
  if (event.type.startsWith("tool.")) {
    const completed = event.type.endsWith("completed");
    return (
      <div className="tool-event">
        <span className={completed ? "tool-state done" : "tool-state running"}>
          {completed ? "✓" : "·"}
        </span>
        <code>{textValue(event.payload.name ?? "tool")}</code>
        <span>
          {textValue(
            event.payload.summary ?? (completed ? "completed" : "running"),
          )}
        </span>
      </div>
    );
  }
  return (
    <div className="system-event">
      <span />
      <strong>{event.type.replaceAll(".", " ")}</strong>
      <small>
        {new Date(event.createdAt).toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
        })}
      </small>
    </div>
  );
}

export function Timeline({
  rounds,
  events,
}: {
  rounds: Round[];
  events: TimelineEvent[];
}) {
  const renderedEvents = visibleEvents(events);
  return (
    <section className="timeline" aria-label="Thread timeline">
      {rounds.map((round) => (
        <div className="round-marker" key={round.id}>
          <span>
            <GitIcon size={14} />
          </span>
          <div>
            <strong>Round {round.sequence}</strong>
            <small>{round.summary || "Snapshot sealed"}</small>
          </div>
          <time>{new Date(round.createdAt).toLocaleString()}</time>
        </div>
      ))}
      {renderedEvents.map((event) => (
        <EventCard event={event} key={event.seq} />
      ))}
      {events.length === 0 ? (
        <div className="timeline-empty">
          <AgentIcon size={22} />
          <p>
            Start a managed Agent run, or invite a collaborator to annotate this
            snapshot.
          </p>
        </div>
      ) : null}
    </section>
  );
}
