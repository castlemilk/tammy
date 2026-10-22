import type { ConveyorBridge } from "electron-conveyor/renderer";
import manifest from "./conveyor-manifest.json" with { type: "json" };
import type { DesktopLaunchScenario } from "./launch-scenario";

export interface SystemDiagnostics {
  readonly apiVersion: string;
  readonly coreVersion: string;
  readonly runtimeMode: "offline";
  readonly networkRequired: false;
}

export type MachineCredentialFileSelection =
  | Readonly<{ selected: false }>
  | Readonly<{ selected: true; handle: string }>;

export function isBoundedUtf8String(value: unknown, maximumBytes: number): value is string {
  if (
    typeof value !== "string" ||
    !Number.isSafeInteger(maximumBytes) ||
    maximumBytes < 0 ||
    value.length > maximumBytes
  )
    return false;
  for (let index = 0; index < value.length; index += 1) {
    const codeUnit = value.charCodeAt(index);
    if (codeUnit >= 0xd800 && codeUnit <= 0xdbff) {
      if (index + 1 >= value.length) return false;
      const trailing = value.charCodeAt(index + 1);
      if (trailing < 0xdc00 || trailing > 0xdfff) return false;
      index += 1;
    } else if (codeUnit >= 0xdc00 && codeUnit <= 0xdfff) return false;
  }
  const encoded = new TextEncoder().encode(value);
  try {
    return encoded.byteLength <= maximumBytes;
  } finally {
    encoded.fill(0);
  }
}

export type ConveyorProcedureKind = "query" | "command";
export type ConveyorManifest = Readonly<
  Record<string, Readonly<Record<string, ConveyorProcedureKind>>>
>;

const EXPECTED_MODULES = Object.freeze([
  "system",
  "workspace",
  "organisation",
  "accounting",
  "banking",
  "basPreparation",
  "documents",
  "purchases",
  "reporting",
  "sbr",
] as const);

function cloneManifest(source: unknown): Record<string, Record<string, ConveyorProcedureKind>> {
  if (typeof source !== "object" || source === null || Array.isArray(source))
    throw new Error("INVALID_CONVEYOR_MANIFEST");
  const modules = Object.keys(source).sort();
  const expectedModules = [...EXPECTED_MODULES].sort();
  if (
    modules.length !== expectedModules.length ||
    modules.some((module, index) => module !== expectedModules[index])
  )
    throw new Error("INVALID_CONVEYOR_MANIFEST");
  const copy: Record<string, Record<string, ConveyorProcedureKind>> = {};
  const names = new Set<string>();
  for (const module of EXPECTED_MODULES) {
    const procedures = (source as Record<string, unknown>)[module];
    if (typeof procedures !== "object" || procedures === null || Array.isArray(procedures))
      throw new Error("INVALID_CONVEYOR_MANIFEST");
    const procedureCopy: Record<string, ConveyorProcedureKind> = {};
    for (const [name, kind] of Object.entries(procedures)) {
      if ((kind !== "query" && kind !== "command") || name.length === 0 || names.has(name))
        throw new Error("INVALID_CONVEYOR_MANIFEST");
      names.add(name);
      procedureCopy[name] = kind;
    }
    if (Object.keys(procedureCopy).length === 0) throw new Error("INVALID_CONVEYOR_MANIFEST");
    copy[module] = procedureCopy;
  }
  return copy;
}

function deepFreeze<T>(value: T): T {
  if (typeof value === "object" && value !== null) {
    for (const child of Object.values(value)) deepFreeze(child);
    Object.freeze(value);
  }
  return value;
}

export const CONVEYOR_MANIFEST: ConveyorManifest = deepFreeze(cloneManifest(manifest));
export const CONVEYOR_PROCEDURE_CHANNELS = Object.freeze(
  EXPECTED_MODULES.map((module) => `conveyor:${module}` as const),
);

export type { ConveyorBridge } from "electron-conveyor/renderer";

declare global {
  interface Window {
    conveyor: ConveyorBridge;
    readonly tammyLaunchScenario?: DesktopLaunchScenario;
  }
}
