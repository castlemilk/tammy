import {
  BookOpen,
  Building2,
  Calculator,
  FileText,
  House,
  Landmark,
  ListTree,
  RadioTower,
  Scale,
  Settings,
  ShieldCheck,
} from "lucide-react";
import type { MouseEvent } from "react";

export const PRIMARY_NAVIGATION = Object.freeze([
  { href: "/overview", icon: House, label: "Overview" },
  { href: "/documents", icon: FileText, label: "Documents" },
  { href: "/banking", icon: Landmark, label: "Banking" },
  { href: "/accounting/chart", icon: ListTree, label: "Chart of accounts" },
  { href: "/accounting/journals", icon: BookOpen, label: "Journals" },
  { href: "/accounting/trial-balance", icon: Scale, label: "Trial balance" },
  { href: "/gst-bas", icon: Calculator, label: "GST & BAS" },
  { href: "/audit", icon: ShieldCheck, label: "Audit trail" },
  { href: "/settings", icon: Settings, label: "Settings" },
] as const);
const SETTINGS_NAVIGATION = PRIMARY_NAVIGATION[8];
const SETTINGS_ROUTES = Object.freeze([
  { href: "/settings/organisation", icon: Building2, label: "Organisation" },
  { href: "/settings/sbr", icon: RadioTower, label: "SBR readiness" },
] as const);

interface NavigationProps {
  readonly activePath: string;
  readonly onNavigate: (path: string) => void;
}

export function Navigation({ activePath, onNavigate }: NavigationProps) {
  return (
    <nav aria-label="Primary" className="flex min-h-0 flex-1 flex-col px-2 pb-3 pt-3">
      <ul className="m-0 grid list-none gap-1 p-0">
        {PRIMARY_NAVIGATION.slice(0, -1).map((item) => (
          <NavigationItem
            activePath={activePath}
            item={item}
            key={item.href}
            onNavigate={onNavigate}
          />
        ))}
      </ul>
      <ul className="m-0 mt-auto list-none border-t border-border px-0 pb-0 pt-3">
        <NavigationItem
          activePath={activePath}
          item={SETTINGS_NAVIGATION}
          onNavigate={onNavigate}
        />
        {activePath.startsWith("/settings") ? (
          <li>
            <ul
              aria-label="Settings"
              className="m-0 mt-1 grid list-none gap-1 p-0 pl-3 max-[900px]:pl-0"
            >
              {SETTINGS_ROUTES.map((item) => (
                <NavigationItem
                  activePath={activePath}
                  item={item}
                  key={item.href}
                  onNavigate={onNavigate}
                />
              ))}
            </ul>
          </li>
        ) : null}
      </ul>
    </nav>
  );
}

function NavigationItem({
  activePath,
  item,
  onNavigate,
}: {
  readonly activePath: string;
  readonly item: (typeof PRIMARY_NAVIGATION)[number] | (typeof SETTINGS_ROUTES)[number];
  readonly onNavigate: (path: string) => void;
}) {
  const path = activePath.split("?", 1)[0] ?? activePath;
  const current = path === item.href;
  const active = current || path.startsWith(`${item.href}/`);
  const Icon = item.icon;
  const navigate = (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    onNavigate(item.href);
  };

  return (
    <li>
      <a
        aria-current={current ? "page" : undefined}
        className={`focus-ring flex min-h-8 items-center gap-2 rounded-[5px] px-2 text-[11px] font-medium leading-4 no-underline transition-colors max-[900px]:justify-center max-[900px]:px-1 ${
          active
            ? "bg-forest text-white"
            : "text-muted-foreground hover:bg-muted hover:text-foreground"
        }`}
        href={item.href}
        onClick={navigate}
        title={item.label}
      >
        <Icon aria-hidden="true" className="size-[13px] shrink-0" strokeWidth={1.8} />
        <span className="max-[900px]:sr-only">{item.label}</span>
      </a>
    </li>
  );
}
