/** The Loupe mark: a magnifier whose lens holds a tiny waterfall. */
export default function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <defs>
        <linearGradient id="logo-grad" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#6366f1" />
          <stop offset="1" stopColor="#06b6d4" />
        </linearGradient>
      </defs>
      <circle cx="13" cy="13" r="9" fill="none" stroke="url(#logo-grad)" strokeWidth="3" />
      {/* three little span bars inside the lens */}
      <rect x="8" y="9" width="7" height="2" rx="1" fill="#6366f1" />
      <rect x="10" y="12" width="7" height="2" rx="1" fill="#0ea5e9" />
      <rect x="12" y="15" width="5" height="2" rx="1" fill="#06b6d4" />
      <path d="M20 20 L27.5 27.5" stroke="url(#logo-grad)" strokeWidth="4" strokeLinecap="round" />
    </svg>
  )
}
