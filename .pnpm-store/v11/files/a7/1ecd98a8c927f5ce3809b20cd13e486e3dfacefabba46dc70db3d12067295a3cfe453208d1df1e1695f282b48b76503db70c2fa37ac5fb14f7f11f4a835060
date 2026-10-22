"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
const node_path_1 = __importDefault(require("node:path"));
const core_utils_1 = require("@electron-forge/core-utils");
const chalk_1 = __importDefault(require("chalk"));
const debug_1 = __importDefault(require("debug"));
const listr2_1 = require("listr2");
const semver_1 = __importDefault(require("semver"));
const install_dependencies_1 = require("../util/install-dependencies");
const read_package_json_1 = require("../util/read-package-json");
const find_template_1 = require("./init-scripts/find-template");
const init_directory_1 = require("./init-scripts/init-directory");
const init_git_1 = require("./init-scripts/init-git");
const init_link_1 = require("./init-scripts/init-link");
const init_npm_1 = require("./init-scripts/init-npm");
const d = (0, debug_1.default)('electron-forge:init');
async function validateTemplate(template, templateModule) {
    if (!templateModule.requiredForgeVersion) {
        throw new Error(`Cannot use a template (${template}) with this version of Electron Forge, as it does not specify its required Forge version.`);
    }
    const forgeVersion = (await (0, read_package_json_1.readRawPackageJson)(node_path_1.default.join(__dirname, '..', '..'))).version;
    if (!semver_1.default.satisfies(forgeVersion, templateModule.requiredForgeVersion)) {
        throw new Error(`Template (${template}) is not compatible with this version of Electron Forge (${forgeVersion}), it requires ${templateModule.requiredForgeVersion}`);
    }
}
exports.default = async ({ dir = process.cwd(), interactive = false, copyCIFiles = false, force = false, template = 'base', skipGit = false, electronVersion = 'latest', }) => {
    d(`Initializing in: ${dir}`);
    const runner = new listr2_1.Listr([
        {
            title: `Resolving package manager`,
            task: async (ctx, task) => {
                ctx.pm = await (0, core_utils_1.resolvePackageManager)();
                task.title = `Resolving package manager: ${chalk_1.default.cyan(ctx.pm.executable)} v${ctx.pm.version}`;
            },
        },
        {
            title: `Resolving template: ${chalk_1.default.cyan(template)}`,
            task: async (ctx, task) => {
                const tmpl = await (0, find_template_1.findTemplate)(template);
                ctx.templateModule = tmpl.template;
                task.output = `Using ${chalk_1.default.green(tmpl.name)} (${tmpl.type} module)`;
            },
            rendererOptions: { persistentOutput: true },
        },
        {
            title: `Resolving Electron version: ${chalk_1.default.cyan(electronVersion)}`,
            task: async (ctx, task) => {
                if (electronVersion === 'latest' ||
                    electronVersion === 'beta' ||
                    electronVersion === 'nightly') {
                    task.output = `Using Electron version tag: ${chalk_1.default.cyan(electronVersion)}`;
                    ctx.parsedElectronVersion = electronVersion;
                }
                else {
                    // semver.clean allows us to accept `v` versions and trims whitespace
                    const maybeVersion = semver_1.default.clean(electronVersion);
                    if (maybeVersion) {
                        task.output = `Using Electron version: ${chalk_1.default.cyan(maybeVersion)}`;
                        ctx.parsedElectronVersion = maybeVersion;
                    }
                    else {
                        throw new Error(`Invalid Electron version: ${electronVersion}. Must be a valid semver version or one of 'latest', 'beta', or 'nightly'.`);
                    }
                }
            },
            rendererOptions: { persistentOutput: true },
        },
        {
            title: 'Initializing directory',
            task: async (_, task) => {
                await (0, init_directory_1.initDirectory)(dir, task, force);
            },
            rendererOptions: { persistentOutput: true },
        },
        {
            title: 'Initializing git repository',
            enabled: !skipGit,
            task: async () => {
                await (0, init_git_1.initGit)(dir);
            },
        },
        {
            title: 'Preparing template',
            task: async ({ templateModule }) => {
                await validateTemplate(template, templateModule);
            },
        },
        {
            title: `Initializing template`,
            task: async ({ templateModule }, task) => {
                if (typeof templateModule.initializeTemplate === 'function') {
                    const tasks = await templateModule.initializeTemplate(dir, {
                        copyCIFiles,
                        force,
                    });
                    if (tasks) {
                        return task.newListr(tasks, { concurrent: false });
                    }
                }
            },
        },
        {
            title: 'Installing template dependencies',
            task: async ({ templateModule }, task) => {
                return task.newListr([
                    {
                        title: 'Installing production dependencies',
                        task: async ({ pm }, task) => {
                            d('installing dependencies');
                            if (templateModule.dependencies?.length) {
                                task.output = `${pm.executable} ${pm.install} ${pm.dev} ${templateModule.dependencies.join(' ')}`;
                            }
                            return await (0, install_dependencies_1.installDependencies)(pm, dir, templateModule.dependencies || [], install_dependencies_1.DepType.PROD, install_dependencies_1.DepVersionRestriction.RANGE);
                        },
                        exitOnError: false,
                    },
                    {
                        title: 'Installing development dependencies',
                        task: async ({ pm }, task) => {
                            d('installing devDependencies');
                            if (templateModule.devDependencies?.length) {
                                task.output = `${pm.executable} ${pm.install} ${pm.dev} ${templateModule.devDependencies.join(' ')}`;
                            }
                            await (0, install_dependencies_1.installDependencies)(pm, dir, templateModule.devDependencies || [], install_dependencies_1.DepType.DEV);
                        },
                        exitOnError: false,
                    },
                    {
                        title: 'Finalizing dependencies',
                        task: async (ctx, task) => {
                            return task.newListr([
                                {
                                    title: 'Installing common dependencies',
                                    task: async ({ pm }, task) => {
                                        await (0, init_npm_1.initNPM)(pm, dir, ctx.parsedElectronVersion, task);
                                    },
                                    exitOnError: false,
                                },
                                {
                                    title: 'Linking Forge dependencies to local build',
                                    enabled: !!process.env.LINK_FORGE_DEPENDENCIES_ON_INIT,
                                    task: async ({ pm }, task) => {
                                        await (0, init_link_1.initLink)(pm, dir, task);
                                    },
                                    exitOnError: true,
                                },
                            ]);
                        },
                    },
                ], {
                    concurrent: false,
                });
            },
        },
    ], {
        concurrent: false,
        silentRendererCondition: !interactive,
        fallbackRendererCondition: Boolean(process.env.DEBUG) || Boolean(process.env.CI),
    });
    await runner.run();
};
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiaW5pdC5qcyIsInNvdXJjZVJvb3QiOiIiLCJzb3VyY2VzIjpbIi4uLy4uL3NyYy9hcGkvaW5pdC50cyJdLCJuYW1lcyI6W10sIm1hcHBpbmdzIjoiOzs7OztBQUFBLDBEQUE2QjtBQUU3QiwyREFBOEU7QUFFOUUsa0RBQTBCO0FBQzFCLGtEQUEwQjtBQUMxQixtQ0FBK0I7QUFDL0Isb0RBQTRCO0FBRTVCLHVFQUlzQztBQUN0QyxpRUFBK0Q7QUFFL0QsZ0VBQTREO0FBQzVELGtFQUE4RDtBQUM5RCxzREFBa0Q7QUFDbEQsd0RBQW9EO0FBQ3BELHNEQUFrRDtBQUVsRCxNQUFNLENBQUMsR0FBRyxJQUFBLGVBQUssRUFBQyxxQkFBcUIsQ0FBQyxDQUFDO0FBb0N2QyxLQUFLLFVBQVUsZ0JBQWdCLENBQzdCLFFBQWdCLEVBQ2hCLGNBQTZCO0lBRTdCLElBQUksQ0FBQyxjQUFjLENBQUMsb0JBQW9CLEVBQUUsQ0FBQztRQUN6QyxNQUFNLElBQUksS0FBSyxDQUNiLDBCQUEwQixRQUFRLDJGQUEyRixDQUM5SCxDQUFDO0lBQ0osQ0FBQztJQUVELE1BQU0sWUFBWSxHQUFHLENBQ25CLE1BQU0sSUFBQSxzQ0FBa0IsRUFBQyxtQkFBSSxDQUFDLElBQUksQ0FBQyxTQUFTLEVBQUUsSUFBSSxFQUFFLElBQUksQ0FBQyxDQUFDLENBQzNELENBQUMsT0FBTyxDQUFDO0lBQ1YsSUFBSSxDQUFDLGdCQUFNLENBQUMsU0FBUyxDQUFDLFlBQVksRUFBRSxjQUFjLENBQUMsb0JBQW9CLENBQUMsRUFBRSxDQUFDO1FBQ3pFLE1BQU0sSUFBSSxLQUFLLENBQ2IsYUFBYSxRQUFRLDREQUE0RCxZQUFZLGtCQUFrQixjQUFjLENBQUMsb0JBQW9CLEVBQUUsQ0FDckosQ0FBQztJQUNKLENBQUM7QUFDSCxDQUFDO0FBRUQsa0JBQWUsS0FBSyxFQUFFLEVBQ3BCLEdBQUcsR0FBRyxPQUFPLENBQUMsR0FBRyxFQUFFLEVBQ25CLFdBQVcsR0FBRyxLQUFLLEVBQ25CLFdBQVcsR0FBRyxLQUFLLEVB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