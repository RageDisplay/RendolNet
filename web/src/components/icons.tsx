import type { SVGProps } from "react";

// Набор иконок: линии 1.5px на сетке 16×16, без заливок.
const base = (d: React.ReactNode) => (p: SVGProps<SVGSVGElement>) => (
  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" {...p}>
    {d}
  </svg>
);

export const IconOverview = base(
  <>
    <path d="M2 13.5V9M6 13.5V5M10 13.5V7.5M14 13.5v-11" />
  </>,
);
export const IconPort = base(
  <>
    <rect x="2" y="4" width="12" height="9" rx="1" />
    <path d="M5 13v-3h6v3M6 4V2.5h4V4" />
  </>,
);
export const IconLease = base(
  <>
    <rect x="2.5" y="2.5" width="11" height="4" rx="1" />
    <rect x="2.5" y="9.5" width="11" height="4" rx="1" />
    <path d="M5 4.5h.01M5 11.5h.01M8 4.5h3M8 11.5h3" />
  </>,
);
export const IconPulse = base(<path d="M1.5 8.5h3l1.5-4 3 8 1.5-4h4" />);
export const IconRoute = base(
  <>
    <circle cx="3.5" cy="12.5" r="1.5" />
    <circle cx="12.5" cy="3.5" r="1.5" />
    <path d="M5 12.5h4.5a2.5 2.5 0 0 0 0-5h-3a2.5 2.5 0 0 1 0-5H11" />
  </>,
);
export const IconShield = base(
  <>
    <path d="M8 1.75 13.5 4v4c0 3.2-2.4 5.4-5.5 6.25C4.9 13.4 2.5 11.2 2.5 8V4z" />
    <path d="M5.75 8 7.4 9.6l3-3.2" />
  </>,
);
export const IconNat = base(
  <>
    <path d="M2 5h10M9.5 2.5 12 5 9.5 7.5M14 11H4M6.5 8.5 4 11l2.5 2.5" />
  </>,
);
export const IconObjects = base(
  <>
    <rect x="2" y="2" width="5" height="5" rx="1" />
    <rect x="9" y="2" width="5" height="5" rx="1" />
    <rect x="2" y="9" width="5" height="5" rx="1" />
    <path d="M11.5 9v5M9 11.5h5" />
  </>,
);
export const IconGuard = base(
  <>
    <path d="M2 8h2.5l1.5-4 3 8 1.5-4H14" />
  </>,
);
export const IconTunnel = base(
  <>
    <path d="M2 13V8a6 6 0 0 1 12 0v5" />
    <path d="M5 13V8.5a3 3 0 0 1 6 0V13" />
  </>,
);
export const IconSessions = base(
  <>
    <path d="M2 4.5h12M2 8h12M2 11.5h7" />
  </>,
);
export const IconLog = base(
  <>
    <path d="M4 2h6l3 3v9H4z" />
    <path d="M10 2v3h3M6.5 8.5h4M6.5 11h4" />
  </>,
);
export const IconConfig = base(
  <>
    <circle cx="4" cy="4" r="1.75" />
    <circle cx="4" cy="12" r="1.75" />
    <circle cx="12" cy="8" r="1.75" />
    <path d="M4 5.75v4.5M5.6 4.7l4.8 2.4" />
  </>,
);
export const IconUpdate = base(
  <>
    <path d="M8 10.5V2M4.75 5.25 8 2l3.25 3.25M2.5 10.5v3h11v-3" />
  </>,
);
export const IconUsers = base(
  <>
    <circle cx="6" cy="5.5" r="2.5" />
    <path d="M1.75 13.5c.5-2.3 2.2-3.5 4.25-3.5s3.75 1.2 4.25 3.5M10.5 3.2a2.4 2.4 0 0 1 0 4.6M12.25 10.2c1 .5 1.7 1.6 2 3.3" />
  </>,
);
export const IconSystem = base(
  <>
    <rect x="2" y="2.5" width="12" height="8" rx="1" />
    <path d="M5.5 13.5h5M8 10.5v3" />
  </>,
);
export const IconPlus = base(<path d="M8 3v10M3 8h10" />);
export const IconTrash = base(
  <>
    <path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 9h5.8l.6-9" />
  </>,
);
export const IconEdit = base(<path d="m10.5 2.5 3 3L6 13H3v-3z" />);
export const IconUp = base(<path d="M8 13V3M4 7l4-4 4 4" />);
export const IconDown = base(<path d="M8 3v10M4 9l4 4 4-4" />);
export const IconX = base(<path d="M4 4l8 8M12 4l-8 8" />);
export const IconSearch = base(
  <>
    <circle cx="7" cy="7" r="4.25" />
    <path d="m10.25 10.25 3.25 3.25" />
  </>,
);
export const IconRefresh = base(<path d="M13 8a5 5 0 1 1-1.5-3.55M13 2.5v2.5h-2.5" />);
export const IconGrip = base(
  <>
    <circle cx="6" cy="4" r=".5" />
    <circle cx="10" cy="4" r=".5" />
    <circle cx="6" cy="8" r=".5" />
    <circle cx="10" cy="8" r=".5" />
    <circle cx="6" cy="12" r=".5" />
    <circle cx="10" cy="12" r=".5" />
  </>,
);
export const IconMenu = base(<path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h11" />);
export const IconCopy = base(
  <>
    <rect x="5.5" y="5.5" width="8" height="8" rx="1" />
    <path d="M10.5 5.5v-3h-8v8h3" />
  </>,
);
export const IconDownload = base(<path d="M8 2v8.5M4.75 7.25 8 10.5l3.25-3.25M2.5 13.5h11" />);
export const IconSun = base(
  <>
    <circle cx="8" cy="8" r="2.75" />
    <path d="M8 1.5v1.5M8 13v1.5M1.5 8H3M13 8h1.5M3.4 3.4l1 1M11.6 11.6l1 1M3.4 12.6l1-1M11.6 4.4l1-1" />
  </>,
);
export const IconMoon = base(<path d="M13 9.5A5.5 5.5 0 0 1 6.5 3a5.5 5.5 0 1 0 6.5 6.5z" />);
export const IconLogout = base(<path d="M6 2.5H3v11h3M10 5l3 3-3 3M13 8H6" />);

export function Logo(p: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 32 32" {...p}>
      <rect width="32" height="32" rx="4" fill="var(--surface-2)" />
      <path d="M8 7h9.5a5.5 5.5 0 0 1 1.6 10.76L24 25h-4.6l-4.3-7H12v7H8zm4 3.6v3.9h5.2a1.95 1.95 0 0 0 0-3.9z" fill="var(--accent)" />
    </svg>
  );
}
