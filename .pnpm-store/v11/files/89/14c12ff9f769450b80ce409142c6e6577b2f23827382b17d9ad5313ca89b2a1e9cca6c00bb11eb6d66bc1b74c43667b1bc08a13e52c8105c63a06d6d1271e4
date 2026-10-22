"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || function (mod) {
    if (mod && mod.__esModule) return mod;
    var result = {};
    if (mod != null) for (var k in mod) if (k !== "default" && Object.prototype.hasOwnProperty.call(mod, k)) __createBinding(result, mod, k);
    __setModuleDefault(result, mod);
    return result;
};
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
const node_path_1 = __importDefault(require("node:path"));
const core_utils_1 = require("@electron-forge/core-utils");
const template_base_1 = __importDefault(require("@electron-forge/template-base"));
const tracer_1 = require("@electron-forge/tracer");
const chalk_1 = __importDefault(require("chalk"));
const debug_1 = __importDefault(require("debug"));
const fs_extra_1 = __importDefault(require("fs-extra"));
const listr2_1 = require("listr2");
const install_dependencies_1 = require("../util/install-dependencies");
const read_package_json_1 = require("../util/read-package-json");
const upgrade_forge_config_1 = __importStar(require("../util/upgrade-forge-config"));
const init_git_1 = require("./init-scripts/init-git");
const init_npm_1 = require("./init-scripts/init-npm");
const d = (0, debug_1.default)('electron-forge:import');
exports.default = (0, tracer_1.autoTrace)({ name: 'import()', category: '@electron-forge/core' }, async (childTrace, { dir = process.cwd(), interactive = false, confirmImport, shouldContinueOnExisting, shouldRemoveDependency, shouldUpdateScript, outDir, skipGit = false, }) => {
    const listrOptions = {
        concurrent: false,
        rendererOptions: {
            collapseSubtasks: false,
            collapseErrors: false,
        },
        silentRendererCondition: !interactive,
        fallbackRendererCondition: Boolean(process.env.DEBUG) || Boolean(process.env.CI),
    };
    const runner = new listr2_1.Listr([
        {
            title: 'Locating importable project',
            task: childTrace({ name: 'locate-project', category: '@electron-forge/core' }, async () => {
                d(`Attempting to import project in: ${dir}`);
                if (!(await fs_extra_1.default.pathExists(dir)) ||
                    !(await fs_extra_1.default.pathExists(node_path_1.default.resolve(dir, 'package.json')))) {
                    throw new Error(`We couldn't find a project with a package.json file in: ${dir}`);
                }
                if (typeof confirmImport === 'function') {
                    if (!(await confirmImport())) {
                        // TODO: figure out if we can just return early here
                        // eslint-disable-next-line no-process-exit
                        process.exit(0);
                    }
                }
                if (!skipGit) {
                    await (0, init_git_1.initGit)(dir);
                }
            }),
        },
        {
            title: 'Processing configuration and dependencies',
            rendererOptions: {
                persistentOutput: true,
                bottomBar: Infinity,
            },
            task: childTrace({ name: 'string', category: 'foo' }, async (_, ctx, task) => {
                const calculatedOutDir = outDir || 'out';
                const importDeps = [].concat(init_npm_1.deps);
                let importDevDeps = [].concat(init_npm_1.devDeps);
                let importExactDevDeps = [].concat(init_npm_1.exactDevDeps);
                let packageJSON = await (0, read_package_json_1.readRawPackageJson)(dir);
                if (!packageJSON.version) {
                    task.output = chalk_1.default.yellow(`Please set the ${chalk_1.default.green('"version"')} in your application's package.json`);
                }
                if (packageJSON.config && packageJSON.config.forge) {
                    if (packageJSON.config.forge.makers) {
                        task.output = chalk_1.default.green('Existing Electron Forge configuration detected');
                        if (typeof shouldContinueOnExisting === 'function') {
                            if (!(await shouldContinueOnExisting())) {
                                // TODO: figure out if we can just return early here
                                // eslint-disable-next-line no-process-exit
                                process.exit(0);
                            }
                        }
                    }
                    else if (!(typeof packageJSON.config.forge === 'object')) {
                        task.output = chalk_1.default.yellow("We can't tell if the Electron Forge config is compatible because it's in an external JavaScript file, not trying to convert it and continuing anyway");
                    }
                    else {
                        d('Upgrading an Electron Forge < 6 project');
                        packageJSON.config.forge = (0, upgrade_forge_config_1.default)(packageJSON.config.forge);
                        importDevDeps = (0, upgrade_forge_config_1.updateUpgradedForgeDevDeps)(packageJSON, importDevDeps);
                    }
                }
                packageJSON.dependencies = packageJSON.dependencies || {};
                packageJSON.devDependencies = packageJSON.devDependencies || {};
                [importDevDeps, importExactDevDeps] = (0, core_utils_1.updateElectronDependency)(packageJSON, importDevDeps, importExactDevDeps);
                const keys = Object.keys(packageJSON.dependencies).concat(Object.keys(packageJSON.devDependencies));
                const buildToolPackages = {
                    '@electron/get': 'already uses this module as a transitive dependency',
                    '@electron/osx-sign': 'already uses this module as a transitive dependency',
                    '@electron/packager': 'already uses this module as a transitive dependency',
                    'electron-builder': 'provides mostly equivalent functionality',
                    'electron-download': 'already uses this module as a transitive dependency',
                    'electron-forge': 'replaced with @electron-forge/cli',
                    'electron-installer-debian': 'already uses this module as a transitive dependency',
                    'electron-installer-dmg': 'already uses this module as a transitive dependency',
                    'electron-installer-flatpak': 'already uses this module as a transitive dependency',
                    'electron-installer-redhat': 'already uses this module as a transitive dependency',
                    'electron-winstaller': 'already uses this module as a transitive dependency',
                };
                for (const key of keys) {
                    if (buildToolPackages[key]) {
                        // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
                        const explanation = buildToolPackages[key];
                        let remove = true;
                        if (typeof shouldRemoveDependency === 'function') {
                            remove = await shouldRemoveDependency(key, explanation);
                        }
                        if (remove) {
                            delete packageJSON.dependencies[key];
                            delete packageJSON.devDependencies[key];
                        }
                    }
                }
                packageJSON.scripts = packageJSON.scripts || {};
                d('reading current scripts object:', packageJSON.scripts);
                const updatePackageScript = async (scriptName, newValue) => {
                    if (packageJSON.scripts[scriptName] !== newValue) {
                        let update = true;
                        if (typeof shouldUpdateScript === 'function') {
                            update = await shouldUpdateScript(scriptName, newValue);
                        }
                        if (update) {
                            packageJSON.scripts[scriptName] = newValue;
                        }
                    }
                };
                await updatePackageScript('start', 'electron-forge start');
                await updatePackageScript('package', 'electron-forge package');
                await updatePackageScript('make', 'electron-forge make');
                d('forgified scripts object:', packageJSON.scripts);
                const writeChanges = async () => {
                    await fs_extra_1.default.writeJson(node_path_1.default.resolve(dir, 'package.json'), packageJSON, { spaces: 2 });
                };
                return task.newListr([
                    {
                        title: `Resolving package manager`,
                        task: async (ctx, task) => {
                            ctx.pm = await (0, core_utils_1.resolvePackageManager)();
                            task.title = `Resolving package manager: ${chalk_1.default.cyan(ctx.pm.executable)}`;
                        },
                    },
                    {
                        title: 'Configuring Yarn (if applicable)',
                        task: async ({ pm }) => {
                            // Yarn v4 defaults to PnP which doesn't work well with CommonJS requires in our forge config
                            // lets ensure that nodeLinker is set to node-modules
                            if (pm.executable === 'yarn') {
                                const yarnrcPath = node_path_1.default.resolve(dir, '.yarnrc.yml');
                                if (!(await fs_extra_1.default.pathExists(yarnrcPath))) {
                                    d('creating .yarnrc.yml with nodeLinker: node-modules');
                                    await fs_extra_1.default.writeFile(yarnrcPath, 'nodeLinker: node-modules\n');
                                }
                            }
                        },
                    },
                    {
                        title: 'Installing dependencies',
                        task: async ({ pm }, task) => {
                            await writeChanges();
                            d('deleting old dependencies forcefully');
                            await fs_extra_1.default.remove(node_path_1.default.resolve(dir, 'node_modules/.bin/electron'));
                            await fs_extra_1.default.remove(node_path_1.default.resolve(dir, 'node_modules/.bin/electron.cmd'));
                            d('installing dependencies');
                            task.output = `${pm.executable} ${pm.install} ${importDeps.join(' ')}`;
                            await (0, install_dependencies_1.installDependencies)(pm, dir, importDeps);
                            d('installing devDependencies');
                            task.output = `${pm.executable} ${pm.install} ${pm.dev} ${importDevDeps.join(' ')}`;
                            await (0, install_dependencies_1.installDependencies)(pm, dir, importDevDeps, install_dependencies_1.DepType.DEV);
                            d('installing devDependencies with exact versions');
                            task.output = `${pm.executable} ${pm.install} ${pm.dev} ${pm.exact} ${importExactDevDeps.join(' ')}`;
                            await (0, install_dependencies_1.installDependencies)(pm, dir, importExactDevDeps, install_dependencies_1.DepType.DEV, install_dependencies_1.DepVersionRestriction.EXACT);
                        },
                    },
                    {
                        title: 'Copying base template Forge configuration',
                        task: async () => {
                            const pathToTemplateConfig = node_path_1.default.resolve(template_base_1.default.templateDir, 'forge.config.js');
                            // if there's an existing config.forge object in package.json
                            if (packageJSON?.config?.forge &&
                                typeof packageJSON.config.forge === 'object') {
                                d('detected existing Forge config in package.json, merging with base template Forge config');
                                packageJSON = await (0, read_package_json_1.readRawPackageJson)(dir);
                                await writeChanges();
                                // otherwise, write to forge.config.js
                            }
                            else {
                                d('writing new forge.config.js');
                                await fs_extra_1.default.copyFile(pathToTemplateConfig, node_path_1.default.resolve(dir, 'forge.config.js'));
                            }
                        },
                    },
                    {
                        title: 'Fixing .gitignore',
                        task: async () => {
                            if (await fs_extra_1.default.pathExists(node_path_1.default.resolve(dir, '.gitignore'))) {
                                const gitignore = await fs_extra_1.default.readFile(node_path_1.default.resolve(dir, '.gitignore'));
                                if (!gitignore.includes(calculatedOutDir)) {
                                    await fs_extra_1.default.writeFile(node_path_1.default.resolve(dir, '.gitignore'), `${gitignore}\n${calculatedOutDir}/`);
                                }
                            }
                        },
                    },
                ], listrOptions);
            }),
        },
        {
            title: 'Finalizing import',
            rendererOptions: {
                persistentOutput: true,
                bottomBar: Infinity,
            },
            task: childTrace({ name: 'finalize-import', category: '@electron-forge/core' }, (_, __, task) => {
                task.output = `We have attempted to convert your app to be in a format that Electron Forge understands.
          
          Thanks for using ${chalk_1.default.green('Electron Forge')}!`;
            }),
        },
    ], listrOptions);
    await runner.run();
});
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiaW1wb3J0LmpzIiwic291cmNlUm9vdCI6IiIsInNvdXJjZXMiOlsiLi4vLi4vc3JjL2FwaS9pbXBvcnQudHMiXSwibmFtZXMiOltdLCJtYXBwaW5ncyI6Ijs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7OztBQUFBLDBEQUE2QjtBQUU3QiwyREFJb0M7QUFLcEMsa0ZBQXlEO0FBQ3pELG1EQUFtRDtBQUNuRCxrREFBMEI7QUFDMUIsa0RBQTBCO0FBQzFCLHdEQUEwQjtBQUMxQixtQ0FBK0I7QUFFL0IsdUVBSXNDO0FBQ3RDLGlFQUErRDtBQUMvRCxxRkFFc0M7QUFFdEMsc0RBQWtEO0FBQ2xELHNEQUFzRTtBQUV0RSxNQUFNLENBQUMsR0FBRyxJQUFBLGVBQUssRUFBQyx1QkFBdUIsQ0FBQyxDQUFDO0FBNkN6QyxrQkFBZSxJQUFBLGtCQUFTLEVBQ3RCLEVBQUUsSUFBSSxFQUFFLFVBQVUsRUFBRSxRQUFRLEVBQUUsc0JBQXNCLEVBQUUsRUFDdEQsS0FBSyxFQUNILFVBQVUsRUFDVixFQUNFLEdBQUcsR0FBRyxPQUFPLENBQUMsR0FBRyxFQUFFLEVBQ25CLFdBQVcsR0FBRyxLQUFLLEVBQ25CLGFBQWEsRUFDYix3QkFBd0IsRUFDeEIsc0JBQXNCLEVBQ3RCLGtCQUFrQixFQUNsQixNQUFNLEVBQ04sT0FBTyxHQUFHLEtBQUssR0FDRCxFQUNELEVBQUU7SUFDakIsTUFBTSxZQUFZLEdBQXlDO1FBQ3pELFVBQVUsRUFBRSxLQUFLO1FBQ2pCLGVBQWUsRUFBRTtZQUNmLGdCQUFnQixFQUFFLEtBQUs7WUFDdkIsY0FBYyxFQUFFLEtBQUs7U0FDdEI7UUFDRCx1QkFBdUIsRUFBRSxDQUFDLFdBQVc7UUFDckMseUJBQXlCLEVBQ3ZCLE9BQU8sQ0FBQyxPQUFPLENBQUMsR0FBRyxDQUFDLEtBQUssQ0FBQyxJQUFJLE9BQU8sQ0FBQyxPQUFPLENBQUMsR0FBRyxDQUFDLEVBQUUsQ0FBQztLQUN4RCxDQUFDO0lBRUYsTUFBTSxNQUFNLEdBQUcsSUFBSSxjQUFLLENBQ3RCO1FB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