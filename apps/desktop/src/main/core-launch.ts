import path from "node:path";

export interface CoreLaunchArgumentOptions {
  readonly isPackaged: boolean;
  readonly userDataPath: string;
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
