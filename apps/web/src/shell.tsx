import {
  BookOpen,
  Calculator,
  ChevronDown,
  FileText,
  House,
  Landmark,
  Lightbulb,
  Link2,
  List,
  ListTree,
  LockKeyhole,
  Scale,
} from "lucide-react";
import { type MouseEvent, type ReactNode, useState } from "react";
import type { Session } from "./types";

export const navigation = [
  { path: "/overview", label: "Overview", icon: House },
  { path: "/documents", label: "Documents", icon: FileText },
  { path: "/banking", label: "Banking", icon: Landmark },
  { path: "/accounts", label: "Chart of accounts", icon: ListTree },
  { path: "/journals", label: "Journals", icon: BookOpen },
  { path: "/trial-balance", label: "Trial balance", icon: Scale },
  { path: "/gst-bas", label: "GST & BAS", icon: Calculator },
  { path: "/intelligence", label: "Intelligence", icon: Lightbulb },
  { path: "/connections", label: "Connections", icon: Link2 },
] as const;

export function AppLink({
  to,
  navigate,
  children,
  className,
  current,
}: {
  to: string;
  navigate: (path: string) => void;
  children: ReactNode;
  className?: string;
  current?: boolean;
}) {
  function click(event: MouseEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    navigate(to);
  }
  return (
    <a aria-current={current ? "page" : undefined} className={className} href={to} onClick={click}>
      {children}
    </a>
  );
}

export function Shell({
  session,
  path,
  navigate,
  onLock,
  children,
}: {
  session: Session;
  path: string;
  navigate: (path: string) => void;
  onLock: () => void;
  children: ReactNode;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const current = navigation.find((item) => item.path === path)?.label ?? "Overview";
  const name = session.organisation?.name || "Your business";
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
  return (
    <div className={`app-shell${menuOpen ? " menu-open" : ""}`}>
      <aside className="sidebar">
        <div className="brand-row">
          <span className="wordmark">Tammy</span>
          <button
            aria-expanded={menuOpen}
            aria-label="Toggle navigation"
            className="icon-button mobile-menu"
            onClick={() => setMenuOpen(!menuOpen)}
            type="button"
          >
            <List />
          </button>
        </div>
        <p className="business-name">{name}</p>
        <nav aria-label="Primary navigation" className="primary-nav">
          {navigation.map((item, index) => {
            const Icon = item.icon;
            return (
              <AppLink
                className={`nav-link${path === item.path ? " active" : ""}${index === 7 ? " nav-section" : ""}`}
                current={path === item.path}
                key={item.path}
                navigate={(next) => {
                  navigate(next);
                  setMenuOpen(false);
                }}
                to={item.path}
              >
                <Icon aria-hidden="true" />
                <span>{item.label}</span>
              </AppLink>
            );
          })}
        </nav>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="breadcrumbs">
            <span>Workspace</span>
            <span aria-hidden="true">/</span>
            <strong>{current}</strong>
          </div>
          <div className="topbar-right">
            <span className="local-status">
              <i />
              Local workspace
            </span>
            <div className="account-menu">
              <button
                aria-expanded={accountOpen}
                className="account-button"
                onClick={() => setAccountOpen(!accountOpen)}
                type="button"
              >
                <span className="avatar">{initials || "T"}</span>
                <span>{name}</span>
                <ChevronDown aria-hidden="true" />
              </button>
              {accountOpen ? (
                <div className="account-popover">
                  <button
                    onClick={() => {
                      setAccountOpen(false);
                      onLock();
                    }}
                    type="button"
                  >
                    <LockKeyhole aria-hidden="true" /> Lock workspace
                  </button>
                </div>
              ) : null}
            </div>
          </div>
        </header>
        <main className="main-content">
          {children}
          <footer className="workspace-footer">
            AUD <span>·</span> Single workspace
          </footer>
        </main>
      </div>
    </div>
  );
}
