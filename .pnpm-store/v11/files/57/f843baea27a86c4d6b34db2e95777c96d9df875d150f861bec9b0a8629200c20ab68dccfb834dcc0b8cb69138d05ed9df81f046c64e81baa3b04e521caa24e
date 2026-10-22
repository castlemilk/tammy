"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.getConfig = void 0;
const vite_1 = require("vite");
const vite_base_config_1 = require("./vite.base.config");
// https://vitejs.dev/config
function getConfig(forgeEnv, userConfig = {}) {
    const { root, mode, forgeConfigSelf } = forgeEnv;
    const name = forgeConfigSelf.name ?? '';
    const config = {
        root,
        mode,
        base: './',
        build: {
            copyPublicDir: true,
            outDir: `.vite/renderer/${name}`,
        },
        plugins: [(0, vite_base_config_1.pluginExposeRenderer)(name)],
        resolve: {
            preserveSymlinks: true,
        },
        clearScreen: false,
    };
    return (0, vite_1.mergeConfig)(config, userConfig);
}
exports.getConfig = getConfig;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoidml0ZS5yZW5kZXJlci5jb25maWcuanMiLCJzb3VyY2VSb290IjoiIiwic291cmNlcyI6WyIuLi8uLi9zcmMvY29uZmlnL3ZpdGUucmVuZGVyZXIuY29uZmlnLnRzIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiI7OztBQUFBLCtCQUFvRTtBQUVwRSx5REFBMEQ7QUFFMUQsNEJBQTRCO0FBQzVCLFNBQWdCLFNBQVMsQ0FDdkIsUUFBK0IsRUFDL0IsYUFBeUIsRUFBRTtJQUUzQixNQUFNLEVBQUUsSUFBSSxFQUFFLElBQUksRUFBRSxlQUFlLEVBQUUsR0FBRyxRQUFRLENBQUM7SUFDakQsTUFBTSxJQUFJLEdBQUcsZUFBZSxDQUFDLElBQUksSUFBSSxFQUFFLENBQUM7SUFFeEMsTUFBTSxNQUFNLEdBQWU7UUFDekIsSUFBSTtRQUNKLElBQUk7UUFDSixJQUFJLEVBQUUsSUFBSTtRQUNWLEtBQUssRUFBRTtZQUNMLGFBQWEsRUFBRSxJQUFJO1lBQ25CLE1BQU0sRUFBRSxrQkFBa0IsSUFBSSxFQUFFO1NBQ2pDO1FBQ0QsT0FBTyxFQUFFLENBQUMsSUFBQSx1Q0FBb0IsRUFBQyxJQUFJLENBQUMsQ0FBQztRQUNyQyxPQUFPLEVBQUU7WUFDUCxnQkFBZ0IsRUFBRSxJQUFJO1NBQ3ZCO1FBQ0QsV0FBVyxFQUFFLEtBQUs7S0FDbkIsQ0FBQztJQUVGLE9BQU8sSUFBQSxrQkFBVyxFQUFDLE1BQU0sRUFBRSxVQUFVLENBQUMsQ0FBQztBQUN6QyxDQUFDO0FBdkJELDhCQXVCQyJ9