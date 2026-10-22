"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.getConfig = void 0;
const vite_1 = require("vite");
const vite_base_config_1 = require("./vite.base.config");
function getConfig(forgeEnv, userConfig = {}) {
    const { forgeConfigSelf } = forgeEnv;
    const define = (0, vite_base_config_1.getBuildDefine)(forgeEnv);
    const config = {
        build: {
            copyPublicDir: false,
            rollupOptions: {
                external: [...vite_base_config_1.external, 'electron/main'],
            },
        },
        plugins: [(0, vite_base_config_1.pluginHotRestart)('restart')],
        define,
        resolve: {
            // Load the Node.js entry.
            conditions: ['node'],
            mainFields: ['module', 'jsnext:main', 'jsnext'],
        },
    };
    const buildConfig = (0, vite_base_config_1.getBuildConfig)(forgeEnv);
    if (userConfig.build?.lib == null) {
        config.build.lib = {
            entry: forgeConfigSelf.entry,
            fileName: () => '[name].js',
            formats: ['cjs'],
        };
    }
    return (0, vite_1.mergeConfig)((0, vite_1.mergeConfig)(buildConfig, config), userConfig);
}
exports.getConfig = getConfig;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoidml0ZS5tYWluLmNvbmZpZy5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uLy4uL3NyYy9jb25maWcvdml0ZS5tYWluLmNvbmZpZy50cyJdLCJuYW1lcyI6W10sIm1hcHBpbmdzIjoiOzs7QUFBQSwrQkFBb0U7QUFFcEUseURBSzRCO0FBRTVCLFNBQWdCLFNBQVMsQ0FDdkIsUUFBNEIsRUFDNUIsYUFBeUIsRUFBRTtJQUUzQixNQUFNLEVBQUUsZUFBZSxFQUFFLEdBQUcsUUFBUSxDQUFDO0lBQ3JDLE1BQU0sTUFBTSxHQUFHLElBQUEsaUNBQWMsRUFBQyxRQUFRLENBQUMsQ0FBQztJQUN4QyxNQUFNLE1BQU0sR0FBZTtRQUN6QixLQUFLLEVBQUU7WUFDTCxhQUFhLEVBQUUsS0FBSztZQUNwQixhQUFhLEVBQUU7Z0JBQ2IsUUFBUSxFQUFFLENBQUMsR0FBRywyQkFBUSxFQUFFLGVBQWUsQ0FBQzthQUN6QztTQUNGO1FBQ0QsT0FBTyxFQUFFLENBQUMsSUFBQSxtQ0FBZ0IsRUFBQyxTQUFTLENBQUMsQ0FBQztRQUN0QyxNQUFNO1FBQ04sT0FBTyxFQUFFO1lBQ1AsMEJBQTBCO1lBQzFCLFVBQVUsRUFBRSxDQUFDLE1BQU0sQ0FBQztZQUNwQixVQUFVLEVBQUUsQ0FBQyxRQUFRLEVBQUUsYUFBYSxFQUFFLFFBQVEsQ0FBQztTQUNoRDtLQUNGLENBQUM7SUFDRixNQUFNLFdBQVcsR0FBRyxJQUFBLGlDQUFjLEVBQUMsUUFBUSxDQUFDLENBQUM7SUFFN0MsSUFBSSxVQUFVLENBQUMsS0FBSyxFQUFFLEdBQUcsSUFBSSxJQUFJLEVBQUUsQ0FBQztRQUNsQyxNQUFNLENBQUMsS0FBTSxDQUFDLEdBQUcsR0FBRztZQUNsQixLQUFLLEVBQUUsZUFBZSxDQUFDLEtBQUs7WUFDNUIsUUFBUSxFQUFFLEdBQUcsRUFBRSxDQUFDLFdBQVc7WUFDM0IsT0FBTyxFQUFFLENBQUMsS0FBSyxDQUFDO1NBQ2pCLENBQUM7SUFDSixDQUFDO0lBRUQsT0FBTyxJQUFBLGtCQUFXLEVBQUMsSUFBQSxrQkFBVyxFQUFDLFdBQVcsRUFBRSxNQUFNLENBQUMsRUFBRSxVQUFVLENBQUMsQ0FBQztBQUNuRSxDQUFDO0FBaENELDhCQWdDQyJ9