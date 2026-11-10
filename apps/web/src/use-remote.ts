import { useEffect, useRef, useState } from "react";
import { errorMessage } from "./utils";

type Remote<T> =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; value: T };

export function useRemote<T>(key: string, load: () => Promise<T>) {
  const loader = useRef(load);
  loader.current = load;
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<Remote<T>>({ status: "loading" });
  // biome-ignore lint/correctness/useExhaustiveDependencies: key and revision deliberately reload a resource.
  useEffect(() => {
    let active = true;
    setState({ status: "loading" });
    void loader.current().then(
      (value) => {
        if (active) setState({ status: "ready", value });
      },
      (error: unknown) => {
        if (active) setState({ status: "error", message: errorMessage(error) });
      },
    );
    return () => {
      active = false;
    };
  }, [key, revision]);
  return { state, reload: () => setRevision((value) => value + 1) };
}
