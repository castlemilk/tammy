import { SquirrelWindowsOptions } from './options';
export { SquirrelWindowsOptions } from './options';
export { SquirrelWindowsOptions as Options } from './options';
/**
 * A utility function to convert SemVer version strings into NuGet-compatible
 * version strings.
 * @param version A SemVer version string
 * @returns A NuGet-compatible version string
 * @see {@link https://semver.org/ | Semantic Versioning specification}
 * @see {@link https://learn.microsoft.com/en-us/nuget/concepts/package-versioning?tabs=semver20sort | NuGet versioning specification}
 */
export declare function convertVersion(version: string): string;
/**
 * Strips characters that NuGet does not allow in the nuspec `<authors>`
 * element. NuGet rejects an `@` character (for example, when the author is an
 * email address) with "The '@' character ... cannot be included in a name",
 * causing the build to fail.
 *
 * @param authors The raw authors value from the package metadata
 * @returns The authors value with disallowed characters removed
 * @see {@link https://github.com/electron/windows-installer/issues/389}
 */
export declare function sanitizeAuthors(authors: string): string;
/**
 * This package's main function, which creates a Squirrel.Windows executable
 * installer and optionally code-signs the output.
 *
 * @param options Options for installer generation and signing
 * @see {@link https://github.com/Squirrel/Squirrel.Windows | Squirrel.Windows}
 */
export declare function createWindowsInstaller(options: SquirrelWindowsOptions): Promise<void>;
