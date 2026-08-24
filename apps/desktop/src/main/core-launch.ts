import path from "node:path";

export interface CoreLaunchArgumentOptions {
  readonly isPackaged: boolean;
  readonly sbrProfilePath?: string;
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
  if (options.sbrProfilePath !== undefined && !path.isAbsolute(options.sbrProfilePath)) {
    throw new Error("SBR_PROFILE_PATH_INVALID");
  }
  const sbrArguments = options.sbrProfilePath
    ? [`--sbr-profile=${path.normalize(options.sbrProfilePath)}`]
    : [];
  if (options.isPackaged) {
    return ["--data-root", path.join(options.userDataPath, "local-core"), ...sbrArguments];
  }
  return [
    "--data-root",
    path.join(options.userDataPath, "local-core-development"),
    "--development-memory-anchors",
    ...sbrArguments,
  ];
}
