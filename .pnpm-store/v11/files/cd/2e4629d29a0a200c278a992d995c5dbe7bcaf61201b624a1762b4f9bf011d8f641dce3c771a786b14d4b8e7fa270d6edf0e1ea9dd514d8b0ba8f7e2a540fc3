import type { VitePluginBuildConfig, VitePluginConfig, VitePluginRendererConfig } from './Config';
import type { UserConfig } from 'vite';
type Target = NonNullable<VitePluginBuildConfig['target']> | 'renderer';
export default class ViteConfigGenerator {
    private readonly pluginConfig;
    private readonly projectDir;
    private readonly isProd;
    constructor(pluginConfig: VitePluginConfig, projectDir: string, isProd: boolean);
    resolveConfig(buildConfig: VitePluginBuildConfig | VitePluginRendererConfig, target: Target): Promise<UserConfig>;
    get mode(): string;
    getBuildConfigs(): Promise<UserConfig[]>;
    getRendererConfig(): Promise<UserConfig[]>;
}
export {};
//# sourceMappingURL=ViteConfig.d.ts.map