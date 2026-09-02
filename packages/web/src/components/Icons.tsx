import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function IconBase({ size = 18, children, ...props }: IconProps) {
  return (
    <svg
      aria-hidden="true"
      fill="none"
      height={size}
      viewBox="0 0 24 24"
      width={size}
      {...props}
    >
      {children}
    </svg>
  );
}

export function CrossIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="M7 4 4 7l5 5-5 5 3 3 5-5 5 5 3-3-5-5 5-5-3-3-5 5-5-5Z"
        fill="currentColor"
      />
    </IconBase>
  );
}

export function ThreadsIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="M4 5.5h16M4 12h16M4 18.5h10"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth="1.8"
      />
      <circle cx="18" cy="18.5" fill="currentColor" r="2" />
    </IconBase>
  );
}

export function PulseIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="M3 12h4l2.2-6 4 12 2-6H21"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.8"
      />
    </IconBase>
  );
}

export function PlusIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="M12 5v14M5 12h14"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth="1.8"
      />
    </IconBase>
  );
}

export function ShareIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <circle cx="18" cy="5" r="2.5" stroke="currentColor" strokeWidth="1.6" />
      <circle cx="6" cy="12" r="2.5" stroke="currentColor" strokeWidth="1.6" />
      <circle cx="18" cy="19" r="2.5" stroke="currentColor" strokeWidth="1.6" />
      <path
        d="m8.2 10.8 7.5-4.4M8.2 13.2l7.5 4.4"
        stroke="currentColor"
        strokeWidth="1.6"
      />
    </IconBase>
  );
}

export function GitIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="m12 3 9 9-9 9-9-9 9-9Z"
        stroke="currentColor"
        strokeLinejoin="round"
        strokeWidth="1.5"
      />
      <circle cx="9" cy="9" fill="currentColor" r="1.7" />
      <circle cx="15" cy="15" fill="currentColor" r="1.7" />
      <path
        d="M9 10.7v2.2c0 1.2 1 2.1 2.1 2.1h2.2M11.8 6.8l5.4 5.4"
        stroke="currentColor"
        strokeWidth="1.4"
      />
    </IconBase>
  );
}

export function AgentIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <rect
        height="13"
        rx="3"
        stroke="currentColor"
        strokeWidth="1.6"
        width="16"
        x="4"
        y="7"
      />
      <path
        d="M12 3v4M8 13h.01M16 13h.01M9 17h6"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth="1.8"
      />
    </IconBase>
  );
}

export function CommentIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="M5 5h14v11H9l-4 3V5Z"
        stroke="currentColor"
        strokeLinejoin="round"
        strokeWidth="1.6"
      />
    </IconBase>
  );
}

export function ChevronIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <path
        d="m9 6 6 6-6 6"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.8"
      />
    </IconBase>
  );
}

export function CopyIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <rect
        height="12"
        rx="2"
        stroke="currentColor"
        strokeWidth="1.5"
        width="12"
        x="8"
        y="8"
      />
      <path
        d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"
        stroke="currentColor"
        strokeWidth="1.5"
      />
    </IconBase>
  );
}

export function StopIcon(props: IconProps) {
  return (
    <IconBase {...props}>
      <rect fill="currentColor" height="10" rx="2" width="10" x="7" y="7" />
    </IconBase>
  );
}
