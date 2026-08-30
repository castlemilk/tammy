export interface ScreenshotCaptureContract {
  readonly buildNumber: string;
  readonly captureArtifactKind: "development-signed-app";
  readonly captureDirectory: string;
  readonly capturedAt: string;
  readonly developmentApp: string;
  readonly developmentSignedAppSha256: string;
  readonly dimensions: { readonly height: 900; readonly width: 1440 };
  readonly fixturePath: string;
  readonly fixtureSha256: string;
  readonly kind: "app-store-screenshot-capture";
  readonly locale: "en-AU";
  readonly marketingVersion: string;
  readonly productSourceCommit: string;
  readonly productSourceTree: string;
  readonly schemaVersion: 1;
  readonly storeMetadataPath: string;
  readonly timezone: "Australia/Melbourne";
  readonly unsignedContentManifestPath: string;
  readonly unsignedContentManifestSha256: string;
}

export function validateScreenshotCaptureContract(value: unknown): ScreenshotCaptureContract;
export function hashAppBundle(app: string): Promise<string>;
export function createCaptureProcessEnvironment(
  source: NodeJS.ProcessEnv,
  additions: { readonly TAMMY_APP_STORE_SCREENSHOT_CONTRACT: string },
): Record<string, string>;
export function authenticateDevelopmentSignedApp(options: {
  readonly developmentApp: string;
  readonly packagingPlan: unknown;
  readonly unsignedManifest: unknown;
  readonly verifySignedCopy?: (...args: unknown[]) => Promise<unknown>;
}): Promise<string>;
