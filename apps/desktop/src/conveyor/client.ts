import { QueryClient } from "@tanstack/react-query";
import { type ConveyorReactClient, createConveyorReactClient } from "electron-conveyor/react";

import type { TammyRouter } from "./router";

/** The one process-local cache. IPC failures stay visible instead of retrying in the background. */
export const queryClient = new QueryClient({
  defaultOptions: {
    mutations: { retry: false },
    queries: { retry: false },
  },
});

export const tammyConveyor = createConveyorReactClient<TammyRouter>({ queryClient });

export type TammyConveyor = ConveyorReactClient<TammyRouter>;

/** A named module slice, including the query keys, hooks, and invalidators on its members. */
export type TammyConveyorModule<Name extends keyof TammyConveyor> = TammyConveyor[Name];
