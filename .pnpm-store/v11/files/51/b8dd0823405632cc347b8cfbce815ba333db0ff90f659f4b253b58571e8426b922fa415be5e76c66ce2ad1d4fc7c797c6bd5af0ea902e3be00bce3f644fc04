"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
const debug_1 = __importDefault(require("debug"));
const vite_1 = require("vite");
const vite_main_config_1 = require("./config/vite.main.config");
const vite_preload_config_1 = require("./config/vite.preload.config");
const vite_renderer_config_1 = require("./config/vite.renderer.config");
const d = (0, debug_1.default)('@electron-forge/plugin-vite:ViteConfig');
class ViteConfigGenerator {
    constructor(pluginConfig, projectDir, isProd) {
        this.pluginConfig = pluginConfig;
        this.projectDir = projectDir;
        this.isProd = isProd;
        d('Config mode:', this.mode);
    }
    async resolveConfig(buildConfig, target) {
        const configEnv = {
            // @see - https://vitejs.dev/config/#conditional-config
            command: this.isProd ? 'build' : 'serve',
            // `mode` affects `.env.[mode]` file load.
            mode: this.mode,
            // Forge extension variables.
            root: this.projectDir,
            forgeConfig: this.pluginConfig,
            forgeConfigSelf: buildConfig,
        };
        // `configEnv` is to be passed as an arguments when the user export a function in `vite.config.js`.
        const userConfig = (await (0, vite_1.loadConfigFromFile)(configEnv, buildConfig.config))
            ?.config;
        switch (target) {
            case 'main':
                return (0, vite_main_config_1.getConfig)(configEnv, userConfig);
            case 'preload':
                return (0, vite_preload_config_1.getConfig)(configEnv, userConfig);
            case 'renderer':
                return (0, vite_renderer_config_1.getConfig)(configEnv, userConfig);
            default:
                throw new Error(`Unknown target: ${target}, expected 'main', 'preload' or 'renderer'`);
        }
    }
    get mode() {
        // Vite's `mode` can be passed in via command.
        // Since we are currently using the JavaScript API, we are opinionated defining two default values for mode here.
        // The `mode` set by the end user in `vite.config.js` has a higher priority.
        return this.isProd ? 'production' : 'development';
    }
    async getBuildConfigs() {
        if (!Array.isArray(this.pluginConfig.build)) {
            throw new Error('"config.build" must be an Array');
        }
        const configs = this.pluginConfig.build
            // Prevent load the default `vite.config.js` file.
            .filter(({ config }) => config)
            .map((buildConfig) => this.resolveConfig(buildConfig, buildConfig.target ?? 'main'));
        return await Promise.all(configs);
    }
    async getRendererConfig() {
        if (!Array.isArray(this.pluginConfig.renderer)) {
            throw new Error('"config.renderer" must be an Array');
        }
        const configs = this.pluginConfig.renderer
            // Prevent load the default `vite.config.js` file.
            .filter(({ config }) => config)
            .map((buildConfig) => this.resolveConfig(buildConfig, 'renderer'));
        return await Promise.all(configs);
    }
}
exports.default = ViteConfigGenerator;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiVml0ZUNvbmZpZy5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uL3NyYy9WaXRlQ29uZmlnLnRzIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiI7Ozs7O0FBQUEsa0RBQTBCO0FBQzFCLCtCQUEwQztBQUUxQyxnRUFBMkU7QUFDM0Usc0VBQWlGO0FBQ2pGLHdFQUFtRjtBQVNuRixNQUFNLENBQUMsR0FBRyxJQUFBLGVBQUssRUFBQyx3Q0FBd0MsQ0FBQyxDQUFDO0FBSTFELE1BQXFCLG1CQUFtQjtJQUN0QyxZQUNtQixZQUE4QixFQUM5QixVQUFrQixFQUNsQixNQUFlO1FBRmYsaUJBQVksR0FBWixZQUFZLENBQWtCO1FBQzlCLGVBQVUsR0FBVixVQUFVLENBQVE7UUFDbEIsV0FBTSxHQUFOLE1BQU0sQ0FBUztRQUVoQyxDQUFDLENBQUMsY0FBYyxFQUFFLElBQUksQ0FBQyxJQUFJLENBQUMsQ0FBQztJQUMvQixDQUFDO0lBRUQsS0FBSyxDQUFDLGFBQWEsQ0FDakIsV0FBNkQsRUFDN0QsTUFBYztRQUVkLE1BQU0sU0FBUyxHQUFjO1lBQzNCLHVEQUF1RDtZQUN2RCxPQUFPLEVBQUUsSUFBSSxDQUFDLE1BQU0sQ0FBQyxDQUFDLENBQUMsT0FBTyxDQUFDLENBQUMsQ0FBQyxPQUFPO1lBQ3hDLDBDQUEwQztZQUMxQyxJQUFJLEVBQUUsSUFBSSxDQUFDLElBQUk7WUFFZiw2QkFBNkI7WUFDN0IsSUFBSSxFQUFFLElBQUksQ0FBQyxVQUFVO1lBQ3JCLFdBQVcsRUFBRSxJQUFJLENBQUMsWUFBWTtZQUM5QixlQUFlLEVBQUUsV0FBVztTQUM3QixDQUFDO1FBRUYsbUdBQW1HO1FBQ25HLE1BQU0sVUFBVSxHQUFHLENBQUMsTUFBTSxJQUFBLHlCQUFrQixFQUFDLFNBQVMsRUFBRSxXQUFXLENBQUMsTUFBTSxDQUFDLENBQUM7WUFDMUUsRUFBRSxNQUFNLENBQUM7UUFDWCxRQUFRLE1BQU0sRUFBRSxDQUFDO1lBQ2YsS0FBSyxNQUFNO2dCQUNULE9BQU8sSUFBQSw0QkFBaUIsRUFBQyxTQUErQixFQUFFLFVBQVUsQ0FBQyxDQUFDO1lBQ3hFLEtBQUssU0FBUztnQkFDWixPQUFPLElBQUEsK0JBQW9CLEVBQ3pCLFNBQStCLEVBQy9CLFVBQVUsQ0FDWCxDQUFDO1lBQ0osS0FBSyxVQUFVO2dCQUNiLE9BQU8sSUFBQSxnQ0FBcUIsRUFDMUIsU0FBa0MsRUFDbEMsVUFBVSxDQUNYLENBQUM7WUFDSjtnQkFDRSxNQUFNLElBQUksS0FBSyxDQUNiLG1CQUFtQixNQUFNLDRDQUE0QyxDQUN0RSxDQUFDO1FBQ04sQ0FBQztJQUNILENBQUM7SUFFRCxJQUFJLElBQUk7UUFDTiw4Q0FBOEM7UUFDOUMsaUhBQWlIO1FBQ2pILDRFQUE0RTtRQUM1RSxPQUFPLElBQUksQ0FBQyxNQUFNLENBQUMsQ0FBQyxDQUFDLFlBQVksQ0FBQyxDQUFDLENBQUMsYUFBYSxDQUFDO0lBQ3BELENBQUM7SUFFRCxLQUFLLENBQUMsZUFBZTtRQUNuQixJQUFJLENBQUMsS0FBSyxDQUFDLE9BQU8sQ0FBQyxJQUFJLENBQUMsWUFBWSxDQUFDLEtBQUssQ0FBQyxFQUFFLENBQUM7WUFDNUMsTUFBTSxJQUFJLEtBQUssQ0FBQyxpQ0FBaUMsQ0FBQyxDQUFDO1FBQ3JELENBQUM7UUFFRCxNQUFNLE9BQU8sR0FBRyxJQUFJLENBQUMsWUFBWSxDQUFDLEtBQUs7WUFDckMsa0RBQWtEO2FBQ2pELE1BQU0sQ0FBQyxDQUFDLEVBQUUsTUFBTSxFQUFFLEVBQUUsRUFBRSxDQUFDLE1BQU0sQ0FBQzthQUM5QixHQUFHLENBQUMsQ0FBQyxXQUFXLEVBQUUsRUFBRSxDQUNuQixJQUFJLENBQUMsYUFBYSxDQUFDLFdBQVcsRUFBRSxXQUFXLENBQUMsTUFBTSxJQUFJLE1BQU0sQ0FBQyxDQUM5RCxDQUFDO1FBRUosT0FBTyxNQUFNLE9BQU8sQ0FBQyxHQUFHLENBQUMsT0FBTyxDQUFDLENBQUM7SUFDcEMsQ0FBQztJQUVELEtBQUssQ0FBQyxpQkFBaUI7UUFDckIsSUFBSSxDQUFDLEtBQUssQ0FBQyxPQUFPLENBQUMsSUFBSSxDQUFDLFlBQVksQ0FBQyxRQUFRLENBQUMsRUFBRSxDQUFDO1lBQy9DLE1BQU0sSUFBSSxLQUFLLENBQUMsb0NBQW9DLENBQUMsQ0FBQztRQUN4RCxDQUFDO1FBRUQsTUFBTSxPQUFPLEdBQUcsSUFBSSxDQUFDLFlBQVksQ0FBQyxRQUFRO1lBQ3hDLGtEQUFrRDthQUNqRCxNQUFNLENBQUMsQ0FBQyxFQUFFLE1BQU0sRUFBRSxFQUFFLEVBQUUsQ0FBQyxNQUFNLENBQUM7YUFDOUIsR0FBRyxDQUFDLENBQUMsV0FBVyxFQUFFLEVBQUUsQ0FBQyxJQUFJLENBQUMsYUFBYSxDQUFDLFdBQVcsRUFBRSxVQUFVLENBQUMsQ0FBQyxDQUFDO1FBRXJFLE9BQU8sTUFBTSxPQUFPLENBQUMsR0FBRyxDQUFDLE9BQU8sQ0FBQyxDQUFDO0lBQ3BDLENBQUM7Q0FDRjtBQWxGRCxzQ0FrRkMifQ==