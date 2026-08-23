import { randomBytes } from "node:crypto";
import { lstat as nodeLstat } from "node:fs/promises";
import path from "node:path";

import type { MachineCredentialFileSelection } from "../shared/desktop-api";
import type { TrustedSbrMainBoundary } from "./rpc-router";

export type SbrFileReleaseKind = "development" | "ordinary-package" | "mas";

export const SBR_BOOKMARK_MAX_BYTES = 64 * 1024;
export const SBR_CREDENTIAL_MAX_BYTES = 4 * 1024 * 1024;
export const SBR_FILE_HANDLE_TTL_MS = 5 * 60 * 1000;

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const textEncoder = new TextEncoder();

interface FileStats {
  readonly size: number;
  isFile(): boolean;
  isSymbolicLink(): boolean;
}

export interface SbrNativeFileDialog {
  showOpenDialog(options: {
    readonly properties: readonly ["openFile"];
    readonly securityScopedBookmarks: boolean;
  }): Promise<{
    readonly bookmarks?: readonly string[];
    readonly canceled: boolean;
    readonly filePaths: readonly string[];
  }>;
}

export interface SbrFileIntake extends TrustedSbrMainBoundary {
  clear(): void;
}

interface SbrFileIntakeOptions {
  readonly createHandle?: () => string;
  readonly decodeBookmark?: (encoded: string) => Uint8Array;
  readonly lstat?: (selectedPath: string) => Promise<FileStats>;
  readonly now?: () => number;
  readonly releaseKind: SbrFileReleaseKind;
  readonly showOpenDialog: SbrNativeFileDialog["showOpenDialog"];
}

interface RetainedSelection {
  readonly bookmark?: Uint8Array;
  readonly expiresAt: number;
  readonly selectedPath: string;
  readonly timer: ReturnType<typeof setTimeout>;
}

interface ProvisionalSelection {
  bookmark: Uint8Array | undefined;
  selectedPath: string | undefined;
}

export class SbrFileIntakeError extends Error {
  public constructor(code: "SBR_FILE_HANDLE_INVALID" | "SBR_FILE_SELECTION_REJECTED") {
    super(code);
    this.name = "SbrFileIntakeError";
  }
}

function createUUIDv7(now = Date.now()): string {
  if (!Number.isSafeInteger(now) || now < 0 || now > 0xffffffffffff) {
    throw new SbrFileIntakeError("SBR_FILE_SELECTION_REJECTED");
  }
  const bytes = randomBytes(16);
  let timestamp = BigInt(now);
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = Number(timestamp & 0xffn);
    timestamp >>= 8n;
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = bytes.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function decodeSbrSecurityScopedBookmark(
  encoded: string,
  options: {
    readonly copy?: (decoded: Uint8Array) => Uint8Array;
    readonly decodeBase64?: (encoded: string) => Buffer;
  } = {},
): Uint8Array {
  if (
    encoded.length === 0 ||
    encoded.length > Math.ceil(SBR_BOOKMARK_MAX_BYTES / 3) * 4 ||
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)
  ) {
    throw new SbrFileIntakeError("SBR_FILE_SELECTION_REJECTED");
  }
  const decoded = (options.decodeBase64 ?? ((value) => Buffer.from(value, "base64")))(encoded);
  try {
    if (
      decoded.byteLength === 0 ||
      decoded.byteLength > SBR_BOOKMARK_MAX_BYTES ||
      decoded.toString("base64") !== encoded
    ) {
      throw new SbrFileIntakeError("SBR_FILE_SELECTION_REJECTED");
    }
    return (options.copy ?? ((value) => new Uint8Array(value)))(decoded);
  } finally {
    decoded.fill(0);
  }
}

export function createSbrFileIntake(options: SbrFileIntakeOptions): Readonly<SbrFileIntake> {
  const entries = new Map<string, RetainedSelection>();
  const provisionalSelections = new Set<ProvisionalSelection>();
  const now = options.now ?? Date.now;
  const lstat = options.lstat ?? nodeLstat;
  const createHandle = options.createHandle ?? (() => createUUIDv7(now()));
  const decode = options.decodeBookmark ?? decodeSbrSecurityScopedBookmark;
  let generation = 0n;

  const clear = (): void => {
    generation += 1n;
    for (const entry of entries.values()) {
      clearTimeout(entry.timer);
      entry.bookmark?.fill(0);
    }
    entries.clear();
    for (const provisional of provisionalSelections) {
      provisional.bookmark?.fill(0);
      provisional.bookmark = undefined;
      provisional.selectedPath = undefined;
    }
    provisionalSelections.clear();
  };

  const forget = (handle: string, entry: RetainedSelection): void => {
    entries.delete(handle);
    clearTimeout(entry.timer);
    entry.bookmark?.fill(0);
  };

  const rejectSelection = (): never => {
    throw new SbrFileIntakeError("SBR_FILE_SELECTION_REJECTED");
  };

  return Object.freeze({
    clear,
    selectMachineCredentialFile: async (): Promise<MachineCredentialFileSelection> => {
      clear();
      const selectionGeneration = generation;
      const provisional: ProvisionalSelection = {
        bookmark: undefined,
        selectedPath: undefined,
      };
      provisionalSelections.add(provisional);
      const isCurrent = (): boolean => generation === selectionGeneration;
      try {
        const isMas = options.releaseKind === "mas";
        const result = await options.showOpenDialog({
          properties: ["openFile"],
          securityScopedBookmarks: isMas,
        });
        if (!isCurrent()) return Object.freeze({ selected: false as const });
        if (result.canceled) return Object.freeze({ selected: false as const });
        if (result.filePaths.length !== 1) return rejectSelection();

        provisional.selectedPath = result.filePaths[0];
        if (
          provisional.selectedPath === undefined ||
          !path.isAbsolute(provisional.selectedPath) ||
          textEncoder.encode(provisional.selectedPath).byteLength > 4_096
        ) {
          return rejectSelection();
        }
        const stats = await lstat(provisional.selectedPath);
        if (!isCurrent() || provisional.selectedPath === undefined) {
          return Object.freeze({ selected: false as const });
        }
        if (
          stats.isSymbolicLink() ||
          !stats.isFile() ||
          !Number.isSafeInteger(stats.size) ||
          stats.size < 1 ||
          stats.size > SBR_CREDENTIAL_MAX_BYTES
        ) {
          return rejectSelection();
        }

        if (isMas) {
          if (result.bookmarks?.length !== 1 || result.bookmarks[0] === undefined) {
            return rejectSelection();
          }
          provisional.bookmark = decode(result.bookmarks[0]);
        } else if (result.bookmarks !== undefined && result.bookmarks.length !== 0) {
          return rejectSelection();
        }

        if (!isCurrent() || provisional.selectedPath === undefined) {
          return Object.freeze({ selected: false as const });
        }
        const handle = createHandle();
        if (!isCurrent()) return Object.freeze({ selected: false as const });
        if (!UUID_V7.test(handle) || entries.has(handle)) return rejectSelection();
        const expiresAt = now() + SBR_FILE_HANDLE_TTL_MS;
        const timer = setTimeout(() => {
          const expired = entries.get(handle);
          if (expired !== undefined) forget(handle, expired);
        }, SBR_FILE_HANDLE_TTL_MS);
        timer.unref?.();
        entries.set(handle, {
          ...(provisional.bookmark === undefined
            ? {}
            : { bookmark: new Uint8Array(provisional.bookmark) }),
          expiresAt,
          selectedPath: provisional.selectedPath,
          timer,
        });
        return Object.freeze({ handle, selected: true as const });
      } catch (error) {
        if (error instanceof SbrFileIntakeError) throw error;
        throw new SbrFileIntakeError("SBR_FILE_SELECTION_REJECTED");
      } finally {
        provisional.bookmark?.fill(0);
        provisional.bookmark = undefined;
        provisional.selectedPath = undefined;
        provisionalSelections.delete(provisional);
      }
    },
    consumeMachineCredentialFile: async (handle: string) => {
      const entry = entries.get(handle);
      if (!UUID_V7.test(handle) || entry === undefined || now() >= entry.expiresAt) {
        if (entry !== undefined) forget(handle, entry);
        throw new SbrFileIntakeError("SBR_FILE_HANDLE_INVALID");
      }
      const consumed = Object.freeze({
        selectedLocalPath: entry.selectedPath,
        ...(entry.bookmark === undefined
          ? {}
          : { securityScopedBookmark: new Uint8Array(entry.bookmark) }),
      });
      forget(handle, entry);
      return consumed;
    },
  });
}
