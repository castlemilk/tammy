import { PluginBase } from '@electron-forge/plugin-base';
import { Listr } from 'listr2';
import type { VitePluginConfig } from './Config';
import type { ForgeListrTask, ForgeMultiHookMap, ResolvedForgeConfig } from '@electron-forge/shared-types';
export default class VitePlugin extends PluginBase<VitePluginConfig> {
    private static alreadyStarted;
    name: string;
    private isProd;
    /**
     * Path to the root of the Electron app
     */
    private projectDir;
    /**
     * Path where Vite output is generated. Usually `${projectDir}/.vite`
     */
    private baseDir;
    private configGeneratorCache;
    private watchers;
    private servers;
    private timeFormatter;
    init: (dir: string) => void;
    setDirectories(dir: string): void;
    private get configGenerator();
    getHooks: () => ForgeMultiHookMap;
    resolveForgeConfig: (forgeConfig: ResolvedForgeConfig) => Promise<ResolvedForgeConfig>;
    packageAfterCopy: (_forgeConfig: ResolvedForgeConfig, buildPath: string) => Promise<void>;
    build: (task?: ForgeListrTask<null>) => Promise<Listr | void>;
    buildRenderer: (task?: ForgeListrTask<null>) => Promise<Listr<NewCtx, any, any> | undefined>;
    launchRendererDevServers: (task?: ForgeListrTask<null>) => Promise<Listr<NewCtx, any, any> | undefined>;
    exitHandler: (options: {
        cleanup?: boolean;
        exit?: boolean;
    }, err?: Error) => void;
}
export { VitePlugin };
//# sourceMappingURL=VitePlugin.d.ts.map