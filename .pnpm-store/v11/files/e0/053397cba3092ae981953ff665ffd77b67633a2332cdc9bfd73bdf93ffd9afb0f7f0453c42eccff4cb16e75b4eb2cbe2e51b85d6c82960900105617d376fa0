"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.getConfig = void 0;
const vite_1 = require("vite");
const vite_base_config_1 = require("./vite.base.config");
function getConfig(forgeEnv, userConfig = {}) {
    const { forgeConfigSelf } = forgeEnv;
    const config = {
        build: {
            copyPublicDir: false,
            rollupOptions: {
                external: [...vite_base_config_1.external, 'electron/renderer'],
                // Preload scripts may contain Web assets, so use the `build.rollupOptions.input` instead `build.lib.entry`.
                input: forgeConfigSelf.entry,
                output: {
                    format: 'cjs',
                    // It should not be split chunks.
                    inlineDynamicImports: true,
                    entryFileNames: '[name].js',
                    chunkFileNames: '[name].js',
                    assetFileNames: '[name].[ext]',
                },
            },
        },
        plugins: [(0, vite_base_config_1.pluginHotRestart)('reload')],
    };
    const buildConfig = (0, vite_base_config_1.getBuildConfig)(forgeEnv);
    return (0, vite_1.mergeConfig)((0, vite_1.mergeConfig)(buildConfig, config), userConfig);
}
exports.getConfig = getConfig;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoidml0ZS5wcmVsb2FkLmNvbmZpZy5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uLy4uL3NyYy9jb25maWcvdml0ZS5wcmVsb2FkLmNvbmZpZy50cyJdLCJuYW1lcyI6W10sIm1hcHBpbmdzIjoiOzs7QUFBQSwrQkFBb0U7QUFFcEUseURBQWdGO0FBRWhGLFNBQWdCLFNBQVMsQ0FDdkIsUUFBNEIsRUFDNUIsYUFBeUIsRUFBRTtJQUUzQixNQUFNLEVBQUUsZUFBZSxFQUFFLEdBQUcsUUFBUSxDQUFDO0lBQ3JDLE1BQU0sTUFBTSxHQUFlO1FBQ3pCLEtBQUssRUFBRTtZQUNMLGFBQWEsRUFBRSxLQUFLO1lBQ3BCLGFBQWEsRUFBRTtnQkFDYixRQUFRLEVBQUUsQ0FBQyxHQUFHLDJCQUFRLEVBQUUsbUJBQW1CLENBQUM7Z0JBQzVDLDRHQUE0RztnQkFDNUcsS0FBSyxFQUFFLGVBQWUsQ0FBQyxLQUFLO2dCQUM1QixNQUFNLEVBQUU7b0JBQ04sTUFBTSxFQUFFLEtBQUs7b0JBQ2IsaUNBQWlDO29CQUNqQyxvQkFBb0IsRUFBRSxJQUFJO29CQUMxQixjQUFjLEVBQUUsV0FBVztvQkFDM0IsY0FBYyxFQUFFLFdBQVc7b0JBQzNCLGNBQWMsRUFBRSxjQUFjO2lCQUMvQjthQUNGO1NBQ0Y7UUFDRCxPQUFPLEVBQUUsQ0FBQyxJQUFBLG1DQUFnQixFQUFDLFFBQVEsQ0FBQyxDQUFDO0tBQ3RDLENBQUM7SUFDRixNQUFNLFdBQVcsR0FBRyxJQUFBLGlDQUFjLEVBQUMsUUFBUSxDQUFDLENBQUM7SUFFN0MsT0FBTyxJQUFBLGtCQUFXLEVBQUMsSUFBQSxrQkFBVyxFQUFDLFdBQVcsRUFBRSxNQUFNLENBQUMsRUFBRSxVQUFVLENBQUMsQ0FBQztBQUNuRSxDQUFDO0FBM0JELDhCQTJCQyJ9