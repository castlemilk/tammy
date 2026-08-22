import path from "node:path";

export interface CoreLaunchArgumentOptions {
  readonly isPackaged: boolean;
  readonly userDataPath: string;
}

export interface LocalLaunchArguments {
  readonly userDataPath?: string;
}

const userDataPrefix = "--user-data-dir=";

export function parseLocalLaunchArguments(arguments_: readonly string[]): LocalLaunchArguments {
  let userDataPath: string | undefined;
  for (const argument of arguments_) {
    if (argument.startsWith(userDataPrefix)) {
      const candidate = argument.slice(userDataPrefix.length);
      if (userDataPath !== undefined || !path.isAbsolute(candidate)) {
        throw new Error("LOCAL_USER_DATA_ARGUMENT_INVALID");
      }
      userDataPath = candidate;
      continue;
    }
    if (argument === "--user-data-dir") {
      throw new Error("LOCAL_USER_DATA_ARGUMENT_INVALID");
    }
    if (argument.startsWith("--sbr-") || argument.startsWith("--tammy-")) {
      throw new Error(`LOCAL_SCENARIO_SWITCH_UNSUPPORTED:${argument.split("=", 1)[0]}`);
    }
  }
  return userDataPath === undefined ? {} : { userDataPath };
}

export function createCoreLaunchArguments(options: CoreLaunchArgumentOptions): readonly string[] {
  if (options.isPackaged) {
    return ["--data-root", path.join(options.userDataPath, "local-core")];
  }
  return [
    "--data-root",
    path.join(options.userDataPath, "local-core-development"),
    "--development-memory-anchors",
  ];
}
