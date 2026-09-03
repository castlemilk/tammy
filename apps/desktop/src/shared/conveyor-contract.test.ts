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

it("validates UTF-8 bounds without allocating from an untrusted maximum", () => {
  expect(isBoundedUtf8String("ok", Number.MAX_SAFE_INTEGER)).toBe(true);
  for (const maximumBytes of [-1, Number.NaN, Number.POSITIVE_INFINITY, 1.5]) {
    expect(isBoundedUtf8String("ok", maximumBytes)).toBe(false);
  }
});
