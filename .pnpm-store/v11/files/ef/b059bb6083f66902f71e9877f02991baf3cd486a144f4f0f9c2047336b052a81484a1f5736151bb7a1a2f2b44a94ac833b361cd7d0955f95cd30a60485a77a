"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.checkSystem = exports.checkPackageManager = void 0;
const node_child_process_1 = require("node:child_process");
const node_os_1 = __importDefault(require("node:os"));
const node_path_1 = __importDefault(require("node:path"));
const core_utils_1 = require("@electron-forge/core-utils");
const debug_1 = __importDefault(require("debug"));
const fs_extra_1 = __importDefault(require("fs-extra"));
const semver_1 = __importDefault(require("semver"));
const d = (0, debug_1.default)('electron-forge:check-system');
async function getGitVersion() {
    return new Promise((resolve) => {
        (0, node_child_process_1.exec)('git --version', (err, output) => err
            ? resolve(null)
            : resolve(output.toString().trim().split(' ').reverse()[0]));
    });
}
/**
 * Packaging an app with Electron Forge requires `node_modules` to be on disk.
 * With `pnpm`, this can be done in a few different ways.
 *
 * `node-linker=hoisted` replicates the behaviour of npm and Yarn Classic, while
 * users may choose to set `public-hoist-pattern` or `hoist-pattern` for advanced
 * configuration purposes.
 */
async function checkPnpmConfig() {
    const { pnpm } = core_utils_1.PACKAGE_MANAGERS;
    const hoistPattern = await (0, core_utils_1.spawnPackageManager)(pnpm, [
        'config',
        'get',
        'hoist-pattern',
    ]);
    const publicHoistPattern = await (0, core_utils_1.spawnPackageManager)(pnpm, [
        'config',
        'get',
        'public-hoist-pattern',
    ]);
    if (hoistPattern !== 'undefined' || publicHoistPattern !== 'undefined') {
        d(`Custom hoist pattern detected ${JSON.stringify({
            hoistPattern,
            publicHoistPattern,
        })}, assuming that the user has configured pnpm to package dependencies.`);
        return;
    }
    const nodeLinker = await (0, core_utils_1.spawnPackageManager)(pnpm, [
        'config',
        'get',
        'node-linker',
    ]);
    if (nodeLinker !== 'hoisted') {
        throw new Error('When using pnpm, `node-linker` must be set to "hoisted" (or a custom `hoist-pattern` or `public-hoist-pattern` must be defined). Run `pnpm config set node-linker hoisted` to set this config value, or add it to your project\'s `.npmrc` file.');
    }
}
async function checkYarnConfig() {
    const { yarn } = core_utils_1.PACKAGE_MANAGERS;
    const yarnVersion = await (0, core_utils_1.spawnPackageManager)(yarn, ['--version']);
    const nodeLinker = await (0, core_utils_1.spawnPackageManager)(yarn, [
        'config',
        'get',
        'nodeLinker',
    ]);
    if (yarnVersion &&
        semver_1.default.gte(yarnVersion, '2.0.0') &&
        nodeLinker !== 'node-modules') {
        throw new Error('When using Yarn 2+, `nodeLinker` must be set to "node-modules". Run `yarn config set nodeLinker node-modules` to set this config value, or add it to your project\'s `.yarnrc.yml` file.');
    }
}
// TODO(erickzhao): Drop antiquated versions of npm for Forge v8
const ALLOWLISTED_VERSIONS = {
    npm: {
        all: '^3.0.0 || ^4.0.0 || ~5.1.0 || ~5.2.0 || >= 5.4.2',
        darwin: '>= 5.4.0',
        linux: '>= 5.4.0',
    },
    yarn: {
        all: '>= 1.0.0',
    },
    pnpm: {
        all: '>= 8.0.0',
    },
};
async function checkPackageManager() {
    const pm = await (0, core_utils_1.resolvePackageManager)();
    const version = pm.version ?? (await (0, core_utils_1.spawnPackageManager)(pm, ['--version']));
    const versionString = version.toString().trim();
    const range = ALLOWLISTED_VERSIONS[pm.executable][process.platform] ??
        ALLOWLISTED_VERSIONS[pm.executable].all;
    if (!semver_1.default.valid(version)) {
        d(`Invalid semver-string while checking version: ${version}`);
        throw new Error(`Could not check ${pm.executable} version "${version}", assuming incompatible`);
    }
    if (!semver_1.default.satisfies(version, range, { includePrerelease: true })) {
        throw new Error(`Incompatible version of ${pm.executable} detected: "${version}" must be in range ${range}`);
    }
    if (pm.executable === 'pnpm') {
        await checkPnpmConfig();
    }
    else if (pm.executable === 'yarn') {
        await checkYarnConfig();
    }
    return `${pm.executable}@${versionString}`;
}
exports.checkPackageManager = checkPackageManager;
/**
 * Some people know their system is OK and don't appreciate the 800ms lag in
 * start up that these checks (in particular the package manager check) costs.
 *
 * Simply creating this flag file in your home directory will skip these checks
 * and shave ~800ms off your forge start time.
 *
 * This is specifically not documented or everyone would make it.
 */
const SKIP_SYSTEM_CHECK = node_path_1.default.resolve(node_os_1.default.homedir(), '.skip-forge-system-check');
async function checkSystem(callerTask) {
    if (!(await fs_extra_1.default.pathExists(SKIP_SYSTEM_CHECK))) {
        d('checking system, create ~/.skip-forge-system-check to stop doing this');
        return callerTask.newListr([
            {
                title: 'Checking git exists',
                // We only call the `initGit` helper in the `init` and `import` commands
                enabled: (ctx) => (ctx.command === 'init' || ctx.command === 'import') && ctx.git,
                task: async (_, task) => {
                    const gitVersion = await getGitVersion();
                    if (gitVersion) {
                        task.title = `Found git@${gitVersion}`;
                    }
                    else {
                        throw new Error('Could not find git in environment');
                    }
                },
            },
            {
                title: 'Checking package manager version',
                task: async (_, task) => {
                    const packageManager = await checkPackageManager();
                    task.title = `Found ${packageManager}`;
                },
            },
        ], {
            concurrent: true,
            exitOnError: true,
            rendererOptions: {
                collapseSubtasks: true,
            },
        });
    }
    d('skipping system check');
    return true;
}
exports.checkSystem = checkSystem;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiY2hlY2stc3lzdGVtLmpzIiwic291cmNlUm9vdCI6IiIsInNvdXJjZXMiOlsiLi4vLi4vc3JjL3V0aWwvY2hlY2stc3lzdGVtLnRzIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiI7Ozs7OztBQUFBLDJEQUEwQztBQUMxQyxzREFBeUI7QUFDekIsMERBQTZCO0FBRTdCLDJEQUtvQztBQUVwQyxrREFBMEI7QUFDMUIsd0RBQTBCO0FBQzFCLG9EQUE0QjtBQUU1QixNQUFNLENBQUMsR0FBRyxJQUFBLGVBQUssRUFBQyw2QkFBNkIsQ0FBQyxDQUFDO0FBRS9DLEtBQUssVUFBVSxhQUFhO0lBQzFCLE9BQU8sSUFBSSxPQUFPLENBQWdCLENBQUMsT0FBTyxFQUFFLEVBQUU7UUFDNUMsSUFBQSx5QkFBSSxFQUFDLGVBQWUsRUFBRSxDQUFDLEdBQUcsRUFBRSxNQUFNLEVBQUUsRUFBRSxDQUNwQyxHQUFHO1lBQ0QsQ0FBQyxDQUFDLE9BQU8sQ0FBQyxJQUFJLENBQUM7WUFDZixDQUFDLENBQUMsT0FBTyxDQUFDLE1BQU0sQ0FBQyxRQUFRLEVBQUUsQ0FBQyxJQUFJLEVBQUUsQ0FBQyxLQUFLLENBQUMsR0FBRyxDQUFDLENBQUMsT0FBTyxFQUFFLENBQUMsQ0FBQyxDQUFDLENBQUMsQ0FDOUQsQ0FBQztJQUNKLENBQUMsQ0FBQyxDQUFDO0FBQ0wsQ0FBQztBQUVEOzs7Ozs7O0dBT0c7QUFDSCxLQUFLLFVBQVUsZUFBZTtJQUM1QixNQUFNLEVBQUUsSUFBSSxFQUFFLEdBQUcsNkJBQWdCLENBQUM7SUFDbEMsTUFBTSxZQUFZLEdBQUcsTUFBTSxJQUFBLGdDQUFtQixFQUFDLElBQUksRUFBRTtRQUNuRCxRQUFRO1FBQ1IsS0FBSztRQUNMLGVBQWU7S0FDaEIsQ0FBQyxDQUFDO0lBQ0gsTUFBTSxrQkFBa0IsR0FBRyxNQUFNLElBQUEsZ0NBQW1CLEVBQUMsSUFBSSxFQUFFO1FBQ3pELFFBQVE7UUFDUixLQUFLO1FBQ0wsc0JBQXNCO0tBQ3ZCLENBQUMsQ0FBQztJQUVILElBQUks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