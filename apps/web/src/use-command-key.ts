import { useRef } from "react";
import { uuidV7 } from "./utils";

// Retain the key after an ambiguous failure so a retry of the same command
// cannot create a second accounting record.
export function useCommandKey() {
  const last = useRef<{ fingerprint: string; key: string } | undefined>(undefined);
  return {
    keyFor(value: unknown) {
      const fingerprint = JSON.stringify(value);
      if (last.current?.fingerprint !== fingerprint) last.current = { fingerprint, key: uuidV7() };
      return last.current.key;
    },
    clear() {
      last.current = undefined;
    },
  };
}
