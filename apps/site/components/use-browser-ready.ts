"use client";

import { useSyncExternalStore } from "react";

const subscribe = () => () => {};
const browserSnapshot = () => true;
const serverSnapshot = () => false;

// Keep native form controls disabled until React can retain their input.
// The server snapshot also covers the first hydration render.
export function useBrowserReady() {
  return useSyncExternalStore(subscribe, browserSnapshot, serverSnapshot);
}
