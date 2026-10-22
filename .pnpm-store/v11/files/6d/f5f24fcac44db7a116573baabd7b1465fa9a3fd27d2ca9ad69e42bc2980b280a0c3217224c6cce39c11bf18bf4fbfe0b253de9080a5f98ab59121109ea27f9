"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.pluginHotRestart = exports.pluginExposeRenderer = exports.getBuildDefine = exports.getDefineKeys = exports.getBuildConfig = exports.external = exports.builtins = void 0;
const node_module_1 = require("node:module");
exports.builtins = [
    'electron',
    'electron/common',
    ...node_module_1.builtinModules.map((m) => [m, `node:${m}`]).flat(),
];
exports.external = [...exports.builtins];
// Used for hot reload after preload scripts.
const viteDevServers = {};
const viteDevServerUrls = {};
function getBuildConfig(env) {
    const { root, mode, command } = env;
    return {
        root,
        mode,
        build: {
            // Prevent multiple builds from interfering with each other.
            emptyOutDir: false,
            // 🚧 Multiple builds may conflict.
            outDir: '.vite/build',
            watch: command === 'serve' ? {} : null,
            minify: command === 'build',
        },
        clearScreen: false,
    };
}
exports.getBuildConfig = getBuildConfig;
function getDefineKeys(names) {
    const define = {};
    // change name from kebab case to upper snake case to agree with vite:define plugin
    // this allows the VitePluginRendererConfig entries to contain names with dashes
    return names.reduce((acc, name) => {
        const NAME = name.toUpperCase().replaceAll('-', '_');
        const keys = {
            VITE_DEV_SERVER_URL: `${NAME}_VITE_DEV_SERVER_URL`,
            VITE_NAME: `${NAME}_VITE_NAME`,
        };
        return { ...acc, [name]: keys };
    }, define);
}
exports.getDefineKeys = getDefineKeys;
function getBuildDefine(env) {
    const { command, forgeConfig } = env;
    const names = forgeConfig.renderer
        .filter(({ name }) => name != null)
        .map(({ name }) => name);
    const defineKeys = getDefineKeys(names);
    const define = Object.entries(defineKeys).reduce((acc, [name, keys]) => {
        const { VITE_DEV_SERVER_URL, VITE_NAME } = keys;
        const def = {
            [VITE_DEV_SERVER_URL]: command === 'serve'
                ? JSON.stringify(viteDevServerUrls[VITE_DEV_SERVER_URL])
                : undefined,
            [VITE_NAME]: JSON.stringify(name),
        };
        return { ...acc, ...def };
    }, {});
    return define;
}
exports.getBuildDefine = getBuildDefine;
function pluginExposeRenderer(name) {
    const { VITE_DEV_SERVER_URL } = getDefineKeys([name])[name];
    return {
        name: '@electron-forge/plugin-vite:expose-renderer',
        configureServer(server) {
            // Expose server for preload scripts hot reload.
            viteDevServers[name] = server;
            server.httpServer?.once('listening', () => {
                const addressInfo = server.httpServer?.address();
                // Expose env constant for main process use.
                viteDevServerUrls[VITE_DEV_SERVER_URL] =
                    `http://localhost:${addressInfo?.port}`;
            });
        },
    };
}
exports.pluginExposeRenderer = pluginExposeRenderer;
function pluginHotRestart(command) {
    return {
        name: '@electron-forge/plugin-vite:hot-restart',
        closeBundle() {
            if (command === 'reload') {
                for (const server of Object.values(viteDevServers)) {
                    // Preload scripts hot reload.
                    server.ws.send({ type: 'full-reload' });
                }
            }
            else if (command === 'restart') {
                // Main process hot restart.
                // https://github.com/electron/forge/blob/v7.2.0/packages/api/core/src/api/start.ts#L216-L223
                // TODO: blocked in #3380
                // process.stdin.emit('data', 'rs');
            }
        },
    };
}
exports.pluginHotRestart = pluginHotRestart;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoidml0ZS5iYXNlLmNvbmZpZy5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uLy4uL3NyYy9jb25maWcvdml0ZS5iYXNlLmNvbmZpZy50cyJdLCJuYW1lcyI6W10sIm1hcHBpbmdzIjoiOzs7QUFBQSw2Q0FBNkM7QUFLaEMsUUFBQSxRQUFRLEdBQUc7SUFDdEIsVUFBVTtJQUNWLGlCQUFpQjtJQUNqQixHQUFHLDRCQUFjLENBQUMsR0FBRyxDQUFDLENBQUMsQ0FBQyxFQUFFLEVBQUUsQ0FBQyxDQUFDLENBQUMsRUFBRSxRQUFRLENBQUMsRUFBRSxDQUFDLENBQUMsQ0FBQyxJQUFJLEVBQUU7Q0FDdEQsQ0FBQztBQUVXLFFBQUEsUUFBUSxHQUFHLENBQUMsR0FBRyxnQkFBUSxDQUFDLENBQUM7QUFFdEMsNkNBQTZDO0FBQzdDLE1BQU0sY0FBYyxHQUFrQyxFQUFFLENBQUM7QUFDekQsTUFBTSxpQkFBaUIsR0FBMkIsRUFBRSxDQUFDO0FBRXJELFNBQWdCLGNBQWMsQ0FBQyxHQUF1QjtJQUNwRCxNQUFNLEVBQUUsSUFBSSxFQUFFLElBQUksRUFBRSxPQUFPLEVBQUUsR0FBRyxHQUFHLENBQUM7SUFFcEMsT0FBTztRQUNMLElBQUk7UUFDSixJQUFJO1FBQ0osS0FBSyxFQUFFO1lBQ0wsNERBQTREO1lBQzVELFdBQVcsRUFBRSxLQUFLO1lBQ2xCLG1DQUFtQztZQUNuQyxNQUFNLEVBQUUsYUFBYTtZQUNyQixLQUFLLEVBQUUsT0FBTyxLQUFLLE9BQU8sQ0FBQyxDQUFDLENBQUMsRUFBRSxDQUFDLENBQUMsQ0FBQyxJQUFJO1lBQ3RDLE1BQU0sRUFBRSxPQUFPLEtBQUssT0FBTztTQUM1QjtRQUNELFdBQVcsRUFBRSxLQUFLO0tBQ25CLENBQUM7QUFDSixDQUFDO0FBaEJELHdDQWdCQztBQUVELFNBQWdCLGFBQWEsQ0FBQyxLQUFlO0lBQzNDLE1BQU0sTUFBTSxHQUE4QyxFQUFFLENBQUM7SUFFN0QsbUZBQW1GO0lBQ25GLGdGQUFnRjtJQUVoRixPQUFPLEtB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