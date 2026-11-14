"use client";
/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */

import { useEffect, useRef, useState } from "react";
import { SiteBrand } from "../site-brand";

const links = [
  { href: "#workflow", label: "How it works" },
  { href: "#features", label: "Features" },
  { href: "#free-tools", label: "Free tools" },
  { href: "#journal", label: "Journal" },
  { href: "#questions", label: "Questions" },
] as const;

/** Sticky landing header. The mobile menu is the only stateful part. */
export function LandingHeader() {
  const [open, setOpen] = useState(false);
  const headerRef = useRef<HTMLElement>(null);
  const navRef = useRef<HTMLElement>(null);
  const menuRef = useRef<HTMLButtonElement>(null);

  // While the menu is open: move focus into it, close on Escape (returning
  // focus to the button) and close on a press anywhere outside the header.
  useEffect(() => {
    if (!open) return;
    navRef.current?.querySelector<HTMLAnchorElement>("a")?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setOpen(false);
      menuRef.current?.focus();
    };
    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && !headerRef.current?.contains(event.target)) setOpen(false);
    };
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("pointerdown", onPointerDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("pointerdown", onPointerDown);
    };
  }, [open]);

  return (
    <header ref={headerRef} className="lp-header" data-menu={open ? "open" : "closed"}>
      <div className="lp-frame lp-header-inner">
        <SiteBrand className="lp-header-brand" />
        <nav ref={navRef} className="lp-header-nav" id="lp-primary-nav" aria-label="Primary navigation">
          {links.map((link) => (
            <a key={link.href} href={link.href} onClick={() => setOpen(false)}>
              {link.label}
            </a>
          ))}
          {/* On phones the header button gives way to the menu, so the link lives here too. */}
          <a className="lp-header-nav-narrow" href="#availability" onClick={() => setOpen(false)}>
            Availability
          </a>
        </nav>
        <div className="lp-header-actions">
          {/* Names where the software under the header runs; motion.tsx sets data-room. */}
          <span className="lp-header-room" aria-hidden="true">
            <span data-room-label="mac">On your Mac</span>
            <span data-room-label="browser">In your browser</span>
            <span data-room-label="reading">Reading</span>
          </span>
          <a className="lp-btn lp-btn--outline lp-btn--sm lp-header-availability" href="#availability">
            Availability
          </a>
          <button
            ref={menuRef}
            type="button"
            className="lp-btn lp-btn--outline lp-btn--sm lp-header-menu"
            aria-expanded={open}
            aria-controls="lp-primary-nav"
            onClick={() => setOpen((value) => !value)}
          >
            {open ? "Close" : "Menu"}
          </button>
        </div>
      </div>
    </header>
  );
}
