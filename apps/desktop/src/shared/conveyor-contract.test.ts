import { expect, it } from "vitest";

import {
  CONVEYOR_MANIFEST,
  CONVEYOR_PROCEDURE_CHANNELS,
  isBoundedUtf8String,
} from "./conveyor-contract";

it("exports a frozen complete eight-module Conveyor contract", () => {
  expect(Object.keys(CONVEYOR_MANIFEST).sort()).toEqual([
    "accounting",
    "banking",
    "documents",
    "organisation",
    "reporting",
    "sbr",
    "system",
    "workspace",
  ]);
  expect(Object.isFrozen(CONVEYOR_MANIFEST)).toBe(true);
  expect(Object.isFrozen(CONVEYOR_MANIFEST.sbr)).toBe(true);
  expect(CONVEYOR_PROCEDURE_CHANNELS).toEqual([
    "conveyor:system",
    "conveyor:workspace",
    "conveyor:organisation",
    "conveyor:accounting",
    "conveyor:banking",
    "conveyor:documents",
    "conveyor:reporting",
    "conveyor:sbr",
  ]);
  expect(isBoundedUtf8String("😀", 4)).toBe(true);
  expect(isBoundedUtf8String("😀", 3)).toBe(false);
});
