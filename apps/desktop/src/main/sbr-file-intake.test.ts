import { mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createSbrFileIntake,
  decodeSbrSecurityScopedBookmark,
  SBR_BOOKMARK_MAX_BYTES,
  SBR_CREDENTIAL_MAX_BYTES,
  SBR_FILE_HANDLE_TTL_MS,
  type SbrFileReleaseKind,
} from "./sbr-file-intake";

const HANDLE = "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb";
const temporaryDirectories: string[] = [];

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

afterEach(async () => {
  vi.useRealTimers();
  await Promise.all(
    temporaryDirectories.splice(0).map((directory) => rm(directory, { recursive: true })),
  );
});

async function credentialFixture(): Promise<string> {
  const directory = await mkdtemp(path.join(os.tmpdir(), "tammy-sbr-intake-"));
  temporaryDirectories.push(directory);
  const credentialPath = path.join(directory, "machine-credential.p12");
  await writeFile(credentialPath, "synthetic credential");
  return credentialPath;
}

function harness(
  releaseKind: SbrFileReleaseKind,
  result: {
    readonly canceled: boolean;
    readonly filePaths: readonly string[];
    readonly bookmarks?: readonly string[];
  },
) {
  const showOpenDialog = vi.fn(async () => result);
  const intake = createSbrFileIntake({
    createHandle: () => HANDLE,
    releaseKind,
    showOpenDialog,
  });
  return { intake, showOpenDialog };
}

describe("SBR credential file intake", () => {
  it("wipes raw decoded bookmark backing when canonical validation rejects it", () => {
    const backing = Buffer.from([9]);

    expect(() =>
      decodeSbrSecurityScopedBookmark("AB==", {
        decodeBase64: () => backing,
      }),
    ).toThrow("SBR_FILE_SELECTION_REJECTED");
    expect(backing).toEqual(Buffer.from([0]));
  });

  it("wipes raw decoded bookmark backing when retained-copy allocation throws", () => {
    const backing = Buffer.from([9, 8, 7]);

    expect(() =>
      decodeSbrSecurityScopedBookmark("CQgH", {
        copy: () => {
          throw new Error("allocation failed");
        },
        decodeBase64: () => backing,
      }),
    ).toThrow("allocation failed");
    expect(backing).toEqual(Buffer.from([0, 0, 0]));
  });

  it("shares the helper's exact four-MiB credential cap", () => {
    expect(SBR_CREDENTIAL_MAX_BYTES).toBe(4 * 1024 * 1024);
  });

  it.each([
    ["development", false],
    ["ordinary-package", false],
    ["mas", true],
  ] as const)(
    "uses the exact %s native chooser policy",
    async (releaseKind, securityScopedBookmarks) => {
      const credentialPath = await credentialFixture();
      const bookmark = Buffer.from("native-bookmark").toString("base64");
      const { intake, showOpenDialog } = harness(releaseKind, {
        ...(releaseKind === "mas" ? { bookmarks: [bookmark] } : {}),
        canceled: false,
        filePaths: [credentialPath],
      });

      await expect(intake.selectMachineCredentialFile()).resolves.toEqual({
        handle: HANDLE,
        selected: true,
      });
      expect(showOpenDialog).toHaveBeenCalledExactlyOnceWith({
        properties: ["openFile"],
        securityScopedBookmarks,
      });

      const consumed = await intake.consumeMachineCredentialFile(HANDLE);
      expect(consumed.selectedLocalPath).toBe(credentialPath);
      if (releaseKind === "mas") {
        expect(Array.from(consumed.securityScopedBookmark ?? [])).toEqual(
          Array.from(new TextEncoder().encode("native-bookmark")),
        );
      } else {
        expect(consumed).not.toHaveProperty("securityScopedBookmark");
      }
    },
  );

  it("returns only an opaque lowercase UUIDv7 and never the selected path or filename", async () => {
    const credentialPath = await credentialFixture();
    const { intake } = harness("ordinary-package", {
      canceled: false,
      filePaths: [credentialPath],
    });

    const selection = await intake.selectMachineCredentialFile();

    expect(selection).toEqual({ selected: true, handle: HANDLE });
    expect(JSON.stringify(selection)).not.toContain(credentialPath);
    expect(JSON.stringify(selection)).not.toContain(path.basename(credentialPath));
    expect(JSON.stringify(selection).length).toBeLessThanOrEqual(128);
  });

  it("uses lstat only for early metadata checks and leaves swap detection to the helper", async () => {
    const credentialPath = await credentialFixture();
    const lstat = vi.fn(async () => ({
      isFile: () => true,
      isSymbolicLink: () => false,
      size: 42,
    }));
    const showOpenDialog = vi.fn(async () => ({ canceled: false, filePaths: [credentialPath] }));
    const intake = createSbrFileIntake({
      createHandle: () => HANDLE,
      lstat,
      releaseKind: "ordinary-package",
      showOpenDialog,
    });

    await intake.selectMachineCredentialFile();
    // No second stat/open/read is performed here. The helper receives the original
    // selected path and performs the authoritative no-follow/stability checks.
    await expect(intake.consumeMachineCredentialFile(HANDLE)).resolves.toEqual({
      selectedLocalPath: credentialPath,
    });
    expect(lstat).toHaveBeenCalledExactlyOnceWith(credentialPath);
  });

  it("rejects symlinks, non-regular files, oversized files, and non-absolute paths", async () => {
    const credentialPath = await credentialFixture();
    const link = path.join(path.dirname(credentialPath), "credential-link.p12");
    await symlink(credentialPath, link);

    for (const [selectedPath, stats] of [
      [link, { isFile: () => true, isSymbolicLink: () => true, size: 10 }],
      [credentialPath, { isFile: () => false, isSymbolicLink: () => false, size: 10 }],
      [
        credentialPath,
        { isFile: () => true, isSymbolicLink: () => false, size: SBR_CREDENTIAL_MAX_BYTES + 1 },
      ],
      ["relative.p12", { isFile: () => true, isSymbolicLink: () => false, size: 10 }],
    ] as const) {
      const intake = createSbrFileIntake({
        createHandle: () => HANDLE,
        lstat: vi.fn(async () => stats),
        releaseKind: "development",
        showOpenDialog: vi.fn(async () => ({ canceled: false, filePaths: [selectedPath] })),
      });
      const error = await intake.selectMachineCredentialFile().catch((caught: unknown) => caught);
      expect(error).toMatchObject({ message: "SBR_FILE_SELECTION_REJECTED" });
      expect(String(error)).not.toContain(selectedPath);
      await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
        "SBR_FILE_HANDLE_INVALID",
      );
    }
  });

  it("consumes a handle once and expires and removes it after at most five minutes", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-24T00:00:00.000Z"));
    const credentialPath = await credentialFixture();
    const { intake } = harness("development", { canceled: false, filePaths: [credentialPath] });

    await intake.selectMachineCredentialFile();
    await expect(intake.consumeMachineCredentialFile(HANDLE)).resolves.toEqual({
      selectedLocalPath: credentialPath,
    });
    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );

    await intake.selectMachineCredentialFile();
    await vi.advanceTimersByTimeAsync(SBR_FILE_HANDLE_TTL_MS);
    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );
  });

  it("clears all retained selections during teardown", async () => {
    const credentialPath = await credentialFixture();
    const { intake } = harness("development", { canceled: false, filePaths: [credentialPath] });
    await intake.selectMachineCredentialFile();

    intake.clear();

    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );
  });

  it("does not repopulate after teardown while the native dialog is pending", async () => {
    const credentialPath = await credentialFixture();
    const dialog = deferred<{ canceled: boolean; filePaths: string[] }>();
    const lstat = vi.fn(async () => ({
      isFile: () => true,
      isSymbolicLink: () => false,
      size: 10,
    }));
    const intake = createSbrFileIntake({
      createHandle: () => HANDLE,
      lstat,
      releaseKind: "development",
      showOpenDialog: () => dialog.promise,
    });
    const selection = intake.selectMachineCredentialFile();

    intake.clear();
    dialog.resolve({ canceled: false, filePaths: [credentialPath] });

    await expect(selection).resolves.toEqual({ selected: false });
    expect(lstat).not.toHaveBeenCalled();
    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );
  });

  it("lets only the latest overlapping chooser retain a path when dialogs resolve in reverse", async () => {
    const firstPath = await credentialFixture();
    const secondPath = await credentialFixture();
    const firstDialog = deferred<{ canceled: boolean; filePaths: string[] }>();
    const secondDialog = deferred<{ canceled: boolean; filePaths: string[] }>();
    const dialogs = [firstDialog, secondDialog];
    const intake = createSbrFileIntake({
      createHandle: () => HANDLE,
      lstat: vi.fn(async () => ({
        isFile: () => true,
        isSymbolicLink: () => false,
        size: 10,
      })),
      releaseKind: "development",
      showOpenDialog: vi.fn(() => {
        const next = dialogs.shift();
        if (next === undefined) throw new Error("unexpected dialog");
        return next.promise;
      }),
    });

    const firstSelection = intake.selectMachineCredentialFile();
    const secondSelection = intake.selectMachineCredentialFile();
    secondDialog.resolve({ canceled: false, filePaths: [secondPath] });
    await expect(secondSelection).resolves.toEqual({ selected: true, handle: HANDLE });
    firstDialog.resolve({ canceled: false, filePaths: [firstPath] });
    await expect(firstSelection).resolves.toEqual({ selected: false });

    await expect(intake.consumeMachineCredentialFile(HANDLE)).resolves.toEqual({
      selectedLocalPath: secondPath,
    });
  });

  it("does not repopulate after teardown while lstat is pending", async () => {
    const credentialPath = await credentialFixture();
    const stats = deferred<{
      isFile(): boolean;
      isSymbolicLink(): boolean;
      size: number;
    }>();
    const lstat = vi.fn(() => stats.promise);
    const intake = createSbrFileIntake({
      createHandle: () => HANDLE,
      lstat,
      releaseKind: "development",
      showOpenDialog: vi.fn(async () => ({ canceled: false, filePaths: [credentialPath] })),
    });
    const selection = intake.selectMachineCredentialFile();
    await vi.waitFor(() => expect(lstat).toHaveBeenCalledOnce());

    intake.clear();
    stats.resolve({ isFile: () => true, isSymbolicLink: () => false, size: 10 });

    await expect(selection).resolves.toEqual({ selected: false });
    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );
  });

  it("wipes the decoded temporary bookmark while retaining only an independent bounded copy", async () => {
    const credentialPath = await credentialFixture();
    const temporaryBookmark = Uint8Array.of(9, 8, 7);
    const intake = createSbrFileIntake({
      createHandle: () => HANDLE,
      decodeBookmark: vi.fn(() => temporaryBookmark),
      releaseKind: "mas",
      showOpenDialog: vi.fn(async () => ({
        bookmarks: ["CQgH"],
        canceled: false,
        filePaths: [credentialPath],
      })),
    });

    await expect(intake.selectMachineCredentialFile()).resolves.toEqual({
      selected: true,
      handle: HANDLE,
    });
    expect(temporaryBookmark).toEqual(Uint8Array.of(0, 0, 0));
    const consumed = await intake.consumeMachineCredentialFile(HANDLE);
    expect(consumed.securityScopedBookmark).toEqual(Uint8Array.of(9, 8, 7));
  });

  it("wipes a decoded bookmark when generation changes reentrantly before storage", async () => {
    const credentialPath = await credentialFixture();
    const temporaryBookmark = Uint8Array.of(6, 5, 4);
    let intake: ReturnType<typeof createSbrFileIntake>;
    intake = createSbrFileIntake({
      createHandle: () => HANDLE,
      decodeBookmark: vi.fn(() => {
        intake.clear();
        return temporaryBookmark;
      }),
      releaseKind: "mas",
      showOpenDialog: vi.fn(async () => ({
        bookmarks: ["BgUE"],
        canceled: false,
        filePaths: [credentialPath],
      })),
    });

    await expect(intake.selectMachineCredentialFile()).resolves.toEqual({ selected: false });
    expect(temporaryBookmark).toEqual(Uint8Array.of(0, 0, 0));
    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );
  });

  it("requires exactly one canonical bounded MAS bookmark and never accepts one outside MAS", async () => {
    const credentialPath = await credentialFixture();
    const tooLarge = Buffer.alloc(SBR_BOOKMARK_MAX_BYTES + 1, 1).toString("base64");
    for (const bookmarks of [undefined, [], ["not base64"], [tooLarge], ["YQ==", "Yg=="]]) {
      const { intake } = harness("mas", {
        ...(bookmarks === undefined ? {} : { bookmarks }),
        canceled: false,
        filePaths: [credentialPath],
      });
      await expect(intake.selectMachineCredentialFile()).rejects.toThrow(
        "SBR_FILE_SELECTION_REJECTED",
      );
    }

    const ordinary = harness("ordinary-package", {
      bookmarks: [Buffer.from("fake-bookmark").toString("base64")],
      canceled: false,
      filePaths: [credentialPath],
    });
    await expect(ordinary.intake.selectMachineCredentialFile()).rejects.toThrow(
      "SBR_FILE_SELECTION_REJECTED",
    );
  });

  it("treats cancel as non-secret state and retains nothing", async () => {
    const { intake } = harness("development", { canceled: true, filePaths: [] });
    await expect(intake.selectMachineCredentialFile()).resolves.toEqual({ selected: false });
    await expect(intake.consumeMachineCredentialFile(HANDLE)).rejects.toThrow(
      "SBR_FILE_HANDLE_INVALID",
    );
  });

  it("has no byte-reading, persistence, logging, or recent-document capability", async () => {
    const source = await readFile(path.resolve(__dirname, "sbr-file-intake.ts"), "utf8");
    expect(source).toContain('lstat as nodeLstat } from "node:fs/promises"');
    expect(source).not.toMatch(
      /(?:\bopen\s*\(|\b(?:readFile|writeFile|appendFile|createReadStream)\b)/,
    );
    expect(source).not.toMatch(/(?:console\.|logger|addRecentDocument|clearRecentDocuments)/);
  });
});
