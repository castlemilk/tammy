"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.VitePlugin = void 0;
// TODO(erickzhao): Remove this when upgrading to Vite 6 and converting to ESM
process.env.VITE_CJS_IGNORE_WARNING = 'true';
const node_path_1 = __importDefault(require("node:path"));
const plugin_base_1 = require("@electron-forge/plugin-base");
const chalk_1 = __importDefault(require("chalk"));
const debug_1 = __importDefault(require("debug"));
const fs_extra_1 = __importDefault(require("fs-extra"));
const listr2_1 = require("listr2");
const vite_1 = __importDefault(require("vite"));
const ViteConfig_1 = __importDefault(require("./ViteConfig"));
const d = (0, debug_1.default)('electron-forge:plugin:vite');
class VitePlugin extends plugin_base_1.PluginBase {
    constructor() {
        super(...arguments);
        this.name = 'vite';
        this.isProd = false;
        this.watchers = [];
        this.servers = [];
        // Matches the format of the default Vite logger
        this.timeFormatter = new Intl.DateTimeFormat(undefined, {
            hour: 'numeric',
            minute: 'numeric',
            second: 'numeric',
        });
        this.init = (dir) => {
            this.setDirectories(dir);
            d('hooking process events');
            process.on('exit', (_code) => {
                this.exitHandler({ cleanup: true });
            });
            process.on('SIGINT', (_signal) => {
                this.exitHandler({ exit: true });
            });
        };
        this.getHooks = () => {
            return {
                preStart: [
                    (0, plugin_base_1.namedHookWithTaskFn)(async (task) => {
                        if (VitePlugin.alreadyStarted)
                            return;
                        VitePlugin.alreadyStarted = true;
                        d(`preStart: removing old content from ${this.baseDir}`);
                        await fs_extra_1.default.remove(this.baseDir);
                        return task?.newListr([
                            {
                                title: 'Launching Vite dev servers for renderer process code...',
                                task: async (_ctx, task) => {
                                    const result = await this.launchRendererDevServers(task);
                                    task.title =
                                        'Launched Vite dev servers for renderer process code';
                                    return result;
                                },
                                rendererOptions: {
                                    persistentOutput: true,
                                    timer: { ...listr2_1.PRESET_TIMER },
                                },
                            },
                            // The main process depends on the `server.port` of the renderer process, so the renderer process is run first.
                            {
                                title: 'Building main process and preload bundles...',
                                task: async (_ctx, task) => {
                                    const result = await this.build(task);
                                    task.title = 'Built main process and preload bundles';
                                    return result;
                                },
                                rendererOptions: {
                                    persistentOutput: true,
                                    timer: { ...listr2_1.PRESET_TIMER },
                                },
                            },
                        ], { concurrent: false });
                    }, 'Preparing Vite bundles'),
                ],
                prePackage: [
                    (0, plugin_base_1.namedHookWithTaskFn)(async (task) => {
                        this.isProd = true;
                        await fs_extra_1.default.remove(this.baseDir);
                        return task?.newListr([
                            {
                                title: 'Building main and preload targets...',
                                task: async (_ctx, subtask) => {
                                    const results = await this.build(subtask);
                                    return results;
                                },
                            },
                            {
                                title: 'Building renderer targets...',
                                task: async (_ctx, subtask) => {
                                    const results = await this.buildRenderer(subtask);
                                    return results;
                                },
                            },
                        ], { concurrent: true });
                    }, 'Building production Vite bundles'),
                ],
                postStart: async (_config, child) => {
                    d('hooking electron process exit');
                    child.on('exit', () => {
                        if (child.restarted)
                            return;
                        this.exitHandler({ cleanup: true, exit: true });
                    });
                },
                resolveForgeConfig: this.resolveForgeConfig,
                packageAfterCopy: this.packageAfterCopy,
            };
        };
        this.resolveForgeConfig = async (forgeConfig) => {
            forgeConfig.packagerConfig ??= {};
            if (forgeConfig.packagerConfig.ignore) {
                if (typeof forgeConfig.packagerConfig.ignore !== 'function') {
                    console.error(chalk_1.default.yellow(`You have set packagerConfig.ignore, the Electron Forge Vite plugin normally sets this automatically.

Your packaged app may be larger than expected if you dont ignore everything other than the '.vite' folder`));
                }
                return forgeConfig;
            }
            forgeConfig.packagerConfig.ignore = (file) => {
                if (!file)
                    return false;
                // `file` always starts with `/`
                // @see - https://github.com/electron/packager/blob/v18.1.3/src/copy-filter.ts#L89-L93
                // Collect the files built by Vite
                return !file.startsWith('/.vite');
            };
            return forgeConfig;
        };
        this.packageAfterCopy = async (_forgeConfig, buildPath) => {
            const pj = await fs_extra_1.default.readJson(node_path_1.default.resolve(this.projectDir, 'package.json'));
            if (!pj.main?.includes('.vite/')) {
                throw new Error(`Electron Forge is configured to use the Vite plugin. The plugin expects the
"main" entry point in "package.json" to be ".vite/*" (where the plugin outputs
the generated files). Instead, it is ${JSON.stringify(pj.main)}.`);
            }
            if (pj.config) {
                delete pj.config.forge;
            }
            await fs_extra_1.default.writeJson(node_path_1.default.resolve(buildPath, 'package.json'), pj, {
                spaces: 2,
            });
        };
        // Main process, Preload scripts and Worker process, etc.
        this.build = async (task) => {
            const configs = await this.configGenerator.getBuildConfigs();
            /**
             * Checks if the result of the Vite build is a Rollup watcher.
             * This should happen iff we're running `electron-forge start`.
             */
            const isRollupWatcher = (x) => x &&
                typeof x === 'object' &&
                'on' in x &&
                typeof x.on === 'function' &&
                'close' in x &&
                typeof x.close === 'function';
            /**
             * Rollup's `input` can be a string, an array of strings, or an object.
             * This function converts the input to a string for the Forge CLI to consume.
             *
             * @see https://rollupjs.org/configuration-options/#input
             */
            const parseInputOptionToString = (input) => {
                if (typeof input === 'string') {
                    return input;
                }
                else if (Array.isArray(input)) {
                    return input.join(' ');
                }
                else {
                    return Object.keys(input).join(' ');
                }
            };
            return task?.newListr(configs.map((userConfig) => {
                let target = '';
                const input = userConfig.build?.rollupOptions?.input;
                if (input) {
                    target = parseInputOptionToString(input);
                }
                else if (typeof userConfig.build?.lib !== 'boolean' &&
                    userConfig.build?.lib?.entry) {
                    target = parseInputOptionToString(userConfig.build.lib.entry);
                }
                return {
                    title: `Building ${chalk_1.default.green(target)} target`,
                    task: async (_ctx, subtask) => {
                        // We wrap this function in a Promise to ensure that the task is marked as completed
                        // only after all bundles are done generated. This is done in the `closeBundle` Rollup hook
                        // rather than when the `vite.build` promise resolves.
                        await new Promise((resolve, reject) => {
                            vite_1.default
                                .build({
                                // Avoid recursive builds caused by users configuring @electron-forge/plugin-vite in Vite config file.
                                configFile: false,
                                // We suppress Vite output and instead log lines using RollupWatcher events
                                logLevel: 'silent',
                                ...userConfig,
                                plugins: [
                                    // This plugin controls the output of the first-time Vite build that happens.
                                    // `buildEnd` and `closeBundle` are Rollup output generation hooks.
                                    // See https://rollupjs.org/plugin-development/#output-generation-hooks
                                    {
                                        name: '@electron-forge/plugin-vite:build-done',
                                        buildEnd(err) {
                                            if (err instanceof Error) {
                                                d('buildEnd rollup hook called with error so build failed');
                                                reject(err);
                                            }
                                        },
                                        closeBundle() {
                                            d('no error in buildEnd and reached closeBundle so build succeeded');
                                            resolve();
                                        },
                                    },
                                    ...(userConfig.plugins ?? []),
                                ],
                                clearScreen: false,
                            })
                                .then((result) => {
                                // When running `start` and enabling watch mode in Vite, the Rollup watcher
                                // emits events for subsequent builds.
                                if (isRollupWatcher(result)) {
                                    result.on('event', (event) => {
                                        if (event.code === 'ERROR' &&
                                            userConfig.logLevel !== 'silent') {
                                            console.error(`\n${chalk_1.default.dim(this.timeFormatter.format(new Date()))} ${event.error.message}`);
                                        }
                                        else if (event.code === 'BUNDLE_END' &&
                                            (!userConfig.logLevel || userConfig.logLevel === 'info')) {
                                            console.log(`${chalk_1.default.dim(this.timeFormatter.format(new Date()))} ${chalk_1.default.cyan.bold('[@electron-forge/plugin-vite]')} ${chalk_1.default.green('target built')} ${chalk_1.default.dim(target)}`);
                                        }
                                    });
                                    this.watchers.push(result);
                                }
                                else {
                                    subtask.title = `Built target ${chalk_1.default.dim(target)}`;
                                }
                                return result;
                            })
                                .catch(reject);
                        });
                    },
                };
            }), {
                concurrent: this.config.concurrent ?? true,
                exitOnError: this.isProd,
            });
        };
        // Renderer process
        this.buildRenderer = async (task) => {
            const rendererConfigs = await this.configGenerator.getRendererConfig();
            return task?.newListr(rendererConfigs.map((userConfig) => ({
                task: async (_ctx, subtask) => {
                    await vite_1.default.build({
                        configFile: false,
                        logLevel: 'error',
                        ...userConfig,
                    });
                    subtask.title = `Built target ${chalk_1.default.dim(node_path_1.default.basename(userConfig.build?.outDir ?? ''))}`;
                },
            })), {
                concurrent: this.config.concurrent ?? true,
            });
        };
        this.launchRendererDevServers = async (task) => {
            const rendererConfigs = await this.configGenerator.getRendererConfig();
            return task?.newListr(rendererConfigs.map((userConfig) => ({
                title: `Target ${chalk_1.default.cyan(node_path_1.default.basename(userConfig.build?.outDir ?? ''))}`,
                task: async (_ctx, subtask) => {
                    const viteDevServer = await vite_1.default.createServer({
                        configFile: false,
                        ...userConfig,
                    });
                    await viteDevServer.listen();
                    const urls = getServerURLs(viteDevServer.resolvedUrls);
                    subtask.output = urls;
                    this.servers.push(viteDevServer);
                    if (viteDevServer.httpServer) {
                        // Make sure that `getDefines` in VitePlugin.ts gets the correct `server.port`. (#3198)
                        const addressInfo = viteDevServer.httpServer.address();
                        const isAddressInfo = (x) => typeof x === 'object' ? typeof x?.address === 'string' : false;
                        if (isAddressInfo(addressInfo)) {
                            userConfig.server ??= {};
                            userConfig.server.port = addressInfo.port;
                        }
                    }
                },
                rendererOptions: {
                    persistentOutput: true,
                },
            })));
        };
        this.exitHandler = (options, err) => {
            d('handling process exit with:', options);
            if (options.cleanup) {
                for (const watcher of this.watchers) {
                    d('cleaning vite watcher');
                    watcher.close();
                }
                this.watchers = [];
                for (const server of this.servers) {
                    d('cleaning http server');
                    server.close();
                }
                this.servers = [];
            }
            if (err)
                console.error(err.stack);
            if (options.exit)
                process.exit(0);
        };
    }
    setDirectories(dir) {
        this.projectDir = dir;
        this.baseDir = node_path_1.default.join(dir, '.vite');
    }
    get configGenerator() {
        return (this.configGeneratorCache ??= new ViteConfig_1.default(this.config, this.projectDir, this.isProd));
    }
}
exports.VitePlugin = VitePlugin;
VitePlugin.alreadyStarted = false;
exports.default = VitePlugin;
/**
 * Get a string for Vite's printServerUrls function without actually printing it.
 * Allows us to set `task.output` to that value without having to pass a custom logger into Vite.
 * @see https://github.com/vitejs/vite/blob/42233d39674be808a6a1a79f1a6e44ed23ba0d61/packages/vite/src/node/logger.ts#L168-L188
 */
function getServerURLs(urls) {
    let output = '';
    const colorUrl = (url) => chalk_1.default.cyan(url.replace(/:(\d+)\//, (_, port) => `:${chalk_1.default.bold(port)}/`));
    for (const url of urls.local) {
        output += `  ${chalk_1.default.green('➜')}  ${chalk_1.default.bold('Local')}:   ${colorUrl(url)}`;
    }
    for (const url of urls.network) {
        output += `  \n${chalk_1.default.green('➜')}  ${chalk_1.default.bold('Network')}: ${colorUrl(url)}`;
    }
    if (urls.network.length === 0) {
        output +=
            chalk_1.default.dim(`  \n${chalk_1.default.green('➜')}  ${chalk_1.default.bold('Network')}: use `) +
                chalk_1.default.bold('--host') +
                chalk_1.default.dim(' to expose');
    }
    return output;
}
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiVml0ZVBsdWdpbi5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uL3NyYy9WaXRlUGx1Z2luLnRzIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiI7Ozs7OztBQUFBLDhFQUE4RTtBQUM5RSxPQUFPLENBQUMsR0FBRyxDQUFDLHVCQUF1QixHQUFHLE1BQU0sQ0FBQztBQUU3QywwREFBNkI7QUFFN0IsNkRBQThFO0FBQzlFLGtEQUEwQjtBQUMxQixrREFBMEI7QUFDMUIsd0RBQTBCO0FBQzFCLG1DQUE2QztBQUM3QyxnREFBdUM7QUFFdkMsOERBQStDO0FBVS9DLE1BQU0sQ0FBQyxHQUFHLElBQUEsZUFBSyxFQUFDLDRCQUE0QixDQUFDLENBQUM7QUFFOUMsTUFBcUIsVUFBVyxTQUFRLHdCQUE0QjtJQUFwRTs7UUFHUyxTQUFJLEdBQUcsTUFBTSxDQUFDO1FBRWIsV0FBTSxHQUFHLEtBQUssQ0FBQztRQWNmLGFBQVEsR0FBZ0MsRUFBRSxDQUFDO1FBRTNDLFlBQU8sR0FBeUIsRUFBRSxDQUFDO1FBRTNDLGdEQUFnRDtRQUN4QyxrQkFBYSxHQUFHLElBQUksSUFBSSxDQUFDLGNBQWMsQ0FBQyxTQUFTLEVBQUU7WUFDekQsSUFBSSxFQUFFLFNBQVM7WUFDZixNQUFNLEVBQUUsU0FBUztZQUNqQixNQUFNLEVBQUUsU0FBUztTQUNsQixDQUFDLENBQUM7UUFFSCxTQUFJLEdBQUcsQ0FBQyxHQUFXLEVBQVEsRUFBRTtZQUMzQixJQUFJLENBQUMsY0FBYyxDQUFDLEdBQUcsQ0FBQyxDQUFDO1lBRXpCLENBQUMsQ0FBQyx3QkFBd0IsQ0FBQyxDQUFDO1lBQzVCLE9BQU8sQ0FBQyxFQUFFLENBQUMsTUFBTSxFQUFFLENBQUMsS0FBSyxFQUFFLEVBQUU7Z0JBQzNCLElBQUksQ0FBQyxXQUFXLENBQUMsRUFBRSxPQUFPLEVBQUUsSUFBSSxFQUFFLENBQUMsQ0FBQztZQUN0QyxDQUFDLENBQUMsQ0FBQztZQUNILE9BQU8sQ0FBQyxFQUFFLENBQUMsUUFBMEIsRUFBRSxD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