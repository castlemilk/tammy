import { randomUUID } from "node:crypto";
import { lstat, open, rename, unlink } from "node:fs/promises";
import path from "node:path";

interface Prepared<Snapshot> {
  readonly snapshot: Snapshot;
  readonly guard: () => void;
  readonly revalidateAuthorisation?: () => Promise<void>;
  readonly filename: string;
  readonly extension: "pdf" | "csv";
}
/** Only trusted acquisition can provide bytes and names. Native selection is the sole path authority. */
export function createValidatedExport<Input, Snapshot>(options: {
  readonly prepare: (request: Input) => Promise<Prepared<Snapshot>>;
  readonly render: (snapshot: Snapshot, extension: "pdf" | "csv") => Promise<Uint8Array>;
  readonly select: (
    filename: string,
    extension: "pdf" | "csv",
  ) => Promise<{ readonly canceled: boolean; readonly filePath?: string }>;
  readonly validateBytes: (bytes: Uint8Array, extension: "pdf" | "csv") => void;
  readonly renderError: (error: unknown) => Error;
  readonly saveError: (error: unknown) => Error;
}) {
  return {
    export: async (request: Input): Promise<{ readonly saved: boolean }> => {
      const { snapshot, guard, filename, extension, revalidateAuthorisation } =
        await options.prepare(request);
      guard();
      let selected: Awaited<ReturnType<typeof options.select>>;
      try {
        selected = await options.select(filename, extension);
      } catch (error) {
        throw options.saveError(error);
      }
      guard();
      if (selected.canceled) return { saved: false };
      try {
        await revalidateAuthorisation?.();
        guard();
      } catch (error) {
        throw options.saveError(error);
      }
      const target = selected.filePath;
      if (
        !target ||
        target.includes("\0") ||
        !path.isAbsolute(target) ||
        !target.toLowerCase().endsWith(`.${extension}`)
      )
        throw options.saveError(undefined);
      const checkTarget = async () => {
        const state = await lstat(target).catch((error: NodeJS.ErrnoException) => {
          if (error.code === "ENOENT") return undefined;
          throw error;
        });
        guard();
        if (state && (!state.isFile() || state.isSymbolicLink()))
          throw options.saveError(undefined);
        const parent = await lstat(path.dirname(target));
        guard();
        if (!parent.isDirectory() || parent.isSymbolicLink()) throw options.saveError(undefined);
      };
      try {
        await checkTarget();
      } catch (error) {
        throw options.saveError(error);
      }
      let bytes: Uint8Array;
      try {
        bytes = await options.render(snapshot, extension);
        guard();
        options.validateBytes(bytes, extension);
      } catch (error) {
        throw options.renderError(error);
      }
      const temporary = path.join(
        path.dirname(target),
        `.tammy-export-${randomUUID()}.${extension}`,
      );
      try {
        const file = await open(temporary, "wx", 0o600);
        try {
          guard();
          await file.writeFile(bytes);
          guard();
          await file.sync();
          guard();
        } finally {
          await file.close();
        }
        guard();
        await checkTarget();
        await revalidateAuthorisation?.();
        guard();
        // No asynchronous boundary between the last authorisation check and atomic activation.
        await rename(temporary, target);
        return { saved: true };
      } catch (error) {
        throw options.saveError(error);
      } finally {
        await unlink(temporary).catch(() => undefined);
      }
    },
  };
}
