import type { AppInfo, DoctorCheck } from "../types";

function CheckRow({ check }: { check: DoctorCheck }) {
  return (
    <li className="doctor-row">
      <span className={`check-icon ${check.status}`} aria-label={check.status}>
        {check.status === "ok" ? "✓" : check.status === "warning" ? "!" : "×"}
      </span>
      <span className="doctor-copy">
        <span>
          <strong>{check.label}</strong>
          {check.optional ? <em>optional</em> : null}
        </span>
        <small>{check.detail}</small>
      </span>
    </li>
  );
}

export function DoctorPanel({ info }: { info?: AppInfo }) {
  const checks = info?.doctor ?? [];
  const ready = checks.filter((check) => check.status === "ok").length;
  return (
    <section className="page page-narrow">
      <header className="page-header">
        <div>
          <p className="eyebrow">Environment</p>
          <h1>Setup & Doctor</h1>
          <p>
            Verify the local runtime. Optional transports degrade gracefully.
          </p>
        </div>
        <span className="readiness-pill">
          {ready}/{checks.length} ready
        </span>
      </header>

      <div className="doctor-summary surface">
        <div className="doctor-score">
          <strong>{ready}</strong>
          <span>checks passing</span>
        </div>
        <div>
          <h2>
            {checks.some((check) => check.status === "error")
              ? "Action required"
              : "Ready to collaborate"}
          </h2>
          <p>
            Core Git and storage checks are required. Tailscale and Tailcat are
            fallback paths and may be unavailable.
          </p>
        </div>
      </div>

      <ul className="doctor-list surface">
        {checks.map((check) => (
          <CheckRow check={check} key={check.key} />
        ))}
        {checks.length === 0 ? (
          <li className="empty-row">Waiting for the host diagnostic report…</li>
        ) : null}
      </ul>

      <div className="doctor-note">
        <span>Connection order</span>
        <strong>LAN</strong>
        <i>→</i>
        <strong>Tailnet</strong>
        <i>→</i>
        <strong>Tailcat</strong>
      </div>
    </section>
  );
}
