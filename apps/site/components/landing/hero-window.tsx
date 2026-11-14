/*
 * The hero's illustrative Tammy window: the desktop rail (one native radio
 * group, so it works unhydrated and arrow keys move between screens), the
 * nine panels, and the lock control in the bar (a checkbox styled as a small
 * button). Everything here is server-rendered; the only script is the tiny
 * rail-track island that keeps the checked chip in view on a phone.
 */
import { AppWindow, Icon } from "./kit";
import { HeroPanels } from "./hero-panels";
import { HeroRailTrack } from "./hero-rail-track";
import { DEFAULT_SCREEN, railGroups } from "./hero-screens";
import { business } from "./scenario";

function LockControl() {
  return (
    <label className="lp-hero-lock">
      <input className="lp-hero-lock-input" type="checkbox" autoComplete="off" />
      <span className="lp-hero-lock-off">Lock the example</span>
      <span className="lp-hero-lock-on">Unlock the example</span>
    </label>
  );
}

export function HeroWindow() {
  return (
    <AppWindow className="lp-hero-window" title={business.name} bar={<LockControl />}>
      <div className="lp-hero-app">
        <fieldset className="lp-hero-rail">
          <legend className="lp-visually-hidden">Choose a screen of the illustrative workspace</legend>
          <HeroRailTrack>
            {railGroups.map((group) => (
              <div className="lp-hero-rail-group" key={group.label}>
                <p className="lp-app-eyebrow lp-hero-rail-label">{group.label}</p>
                {group.items.map((item) => (
                  <label className="lp-hero-rail-item" key={item.id}>
                    <input
                      className="lp-hero-rail-input"
                      type="radio"
                      name="lp-hero-screen"
                      value={item.id}
                      defaultChecked={item.id === DEFAULT_SCREEN}
                      autoComplete="off"
                    />
                    <Icon name={item.icon} />
                    <span>{item.label}</span>
                  </label>
                ))}
              </div>
            ))}
          </HeroRailTrack>
        </fieldset>
        <HeroPanels />
      </div>
    </AppWindow>
  );
}
