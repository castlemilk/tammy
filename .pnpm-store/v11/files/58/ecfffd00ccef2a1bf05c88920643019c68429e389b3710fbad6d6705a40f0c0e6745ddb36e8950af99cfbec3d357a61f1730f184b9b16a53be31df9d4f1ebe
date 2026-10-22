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
exports.renderConfigTemplate = exports.forgeConfigIsValidFilePath = exports.fromBuildIdentifier = exports.unregisterForgeConfigForDirectory = exports.registerForgeConfigForDirectory = exports.registeredForgeConfigs = void 0;
const node_path_1 = __importDefault(require("node:path"));
const eta_1 = require("eta");
const fs_extra_1 = __importDefault(require("fs-extra"));
const interpret = __importStar(require("interpret"));
const jiti_1 = require("jiti");
const rechoir = __importStar(require("rechoir"));
// eslint-disable-next-line n/no-missing-import
const dynamic_import_js_1 = require("../../helper/dynamic-import.js");
const hook_1 = require("./hook");
const plugin_interface_1 = __importDefault(require("./plugin-interface"));
const read_package_json_1 = require("./read-package-json");
const underscoreCase = (str) => str
    .replace(/(.)([A-Z][a-z]+)/g, '$1_$2')
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .toUpperCase();
/* eslint-disable @typescript-eslint/no-explicit-any */
function isBuildIdentifierConfig(value) {
    return (value && typeof value === 'object' && value.__isMagicBuildIdentifierMap);
}
const proxify = (buildIdentifier, proxifiedObject, envPrefix) => {
    let newObject = {};
    if (Array.isArray(proxifiedObject)) {
        newObject = [];
    }
    for (const [key, val] of Object.entries(proxifiedObject)) {
        if (typeof val === 'object' &&
            (val.constructor === Object || val.constructor === Array) &&
            key !== 'pluginInterface' &&
            !(val instanceof RegExp)) {
            newObject[key] = proxify(buildIdentifier, proxifiedObject[key], `${envPrefix}_${underscoreCase(key)}`);
        }
        else {
            newObject[key] = proxifiedObject[key];
        }
    }
    return new Proxy(newObject, {
        get(target, name, receiver) {
            // eslint-disable-next-line no-prototype-builtins
            if (!target.hasOwnProperty(name) && typeof name === 'string') {
                const envValue = process.env[`${envPrefix}_${underscoreCase(name)}`];
                if (envValue)
                    return envValue;
            }
            const value = Reflect.get(target, name, receiver);
            if (isBuildIdentifierConfig(value)) {
                const identifier = typeof buildIdentifier === 'function'
                    ? buildIdentifier()
                    : buildIdentifier;
                return value.map[identifier];
            }
            return value;
        },
        getOwnPropertyDescriptor(target, name) {
            const envValue = process.env[`${envPrefix}_${underscoreCase(name)}`];
            // eslint-disable-next-line no-prototype-builtins
            if (target.hasOwnProperty(name)) {
                return Reflect.getOwnPropertyDescriptor(target, name);
            }
            if (envValue) {
                return {
                    writable: true,
                    enumerable: true,
                    configurable: true,
                    value: envValue,
                };
            }
            return undefined;
        },
    });
};
/* eslint-enable @typescript-eslint/no-explicit-any */
exports.registeredForgeConfigs = new Map();
function registerForgeConfigForDirectory(dir, config) {
    exports.registeredForgeConfigs.set(node_path_1.default.resolve(dir), config);
}
exports.registerForgeConfigForDirectory = registerForgeConfigForDirectory;
function unregisterForgeConfigForDirectory(dir) {
    exports.registeredForgeConfigs.delete(node_path_1.default.resolve(dir));
}
exports.unregisterForgeConfigForDirectory = unregisterForgeConfigForDirectory;
function fromBuildIdentifier(map) {
    return {
        map,
        __isMagicBuildIdentifierMap: true,
    };
}
exports.fromBuildIdentifier = fromBuildIdentifier;
async function forgeConfigIsValidFilePath(dir, forgeConfig) {
    return (typeof forgeConfig === 'string' &&
        ((await fs_extra_1.default.pathExists(node_path_1.default.resolve(dir, forgeConfig))) ||
            fs_extra_1.default.pathExists(node_path_1.default.resolve(dir, `${forgeConfig}.js`))));
}
exports.forgeConfigIsValidFilePath = forgeConfigIsValidFilePath;
const eta = new eta_1.Eta({ useWith: true, autoEscape: false, autoTrim: false });
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function renderConfigTemplate(dir, templateObj, obj) {
    for (const [key, value] of Object.entries(obj)) {
        if (typeof value === 'object' && value !== null) {
            renderConfigTemplate(dir, templateObj, value);
        }
        else if (typeof value === 'string') {
            obj[key] = eta.renderString(value, templateObj);
            if (obj[key].startsWith('require:')) {
                // eslint-disable-next-line @typescript-eslint/no-require-imports
                obj[key] = require(node_path_1.default.resolve(dir, obj[key].substr(8)));
            }
        }
    }
}
exports.renderConfigTemplate = renderConfigTemplate;
exports.default = async (dir) => {
    let forgeConfig = exports.registeredForgeConfigs.get(dir);
    const packageJSON = await (0, read_package_json_1.readRawPackageJson)(dir);
    if (forgeConfig === undefined) {
        forgeConfig =
            packageJSON.config && packageJSON.config.forge
                ? packageJSON.config.forge
                : null;
    }
    if (!forgeConfig || typeof forgeConfig === 'string') {
        // interpret.extensions doesn't support `.mts` files
        for (const extension of [
            '.js',
            '.mts',
            ...Object.keys(interpret.extensions),
        ]) {
            const pathToConfig = node_path_1.default.resolve(dir, `forge.config${extension}`);
            if (await fs_extra_1.default.pathExists(pathToConfig)) {
                // Use rechoir to parse alternative syntaxes (except for TypeScript where we use jiti)
                if (!['.cts', '.mts', '.ts'].includes(extension)) {
                    rechoir.prepare(interpret.extensions, pathToConfig, dir);
                }
                forgeConfig = `forge.config${extension}`;
                break;
            }
        }
    }
    forgeConfig = forgeConfig || {};
    if (await forgeConfigIsValidFilePath(dir, forgeConfig)) {
        const forgeConfigPath = node_path_1.default.resolve(dir, forgeConfig);
        try {
            let loadFn;
            if (['.cts', '.mts', '.ts'].includes(node_path_1.default.extname(forgeConfigPath))) {
                const jiti = (0, jiti_1.createJiti)(__filename);
                loadFn = jiti.import;
            }
            else {
                loadFn = dynamic_import_js_1.dynamicImportMaybe;
            }
            // The loaded "config" could potentially be a static forge config, ESM module or async function
            const loaded = (await loadFn(forgeConfigPath));
            const maybeForgeConfig = 'default' in loaded ? loaded.default : loaded;
            forgeConfig =
                typeof maybeForgeConfig === 'function'
                    ? await maybeForgeConfig()
                    : maybeForgeConfig;
        }
        catch (err) {
            console.error(`Failed to load: ${forgeConfigPath}`);
            throw err;
        }
    }
    else if (typeof forgeConfig !== 'object') {
        throw new Error('Expected packageJSON.config.forge to be an object or point to a requirable JS file');
    }
    const defaultForgeConfig = {
        rebuildConfig: {},
        packagerConfig: {},
        makers: [],
        publishers: [],
        plugins: [],
    };
    let resolvedForgeConfig = {
        ...defaultForgeConfig,
        ...forgeConfig,
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        pluginInterface: null,
    };
    const templateObj = { ...packageJSON, year: new Date().getFullYear() };
    renderConfigTemplate(dir, templateObj, resolvedForgeConfig);
    resolvedForgeConfig.pluginInterface = await plugin_interface_1.default.create(dir, resolvedForgeConfig);
    resolvedForgeConfig = await (0, hook_1.runMutatingHook)(resolvedForgeConfig, 'resolveForgeConfig', resolvedForgeConfig);
    return proxify(resolvedForgeConfig.buildIdentifier || '', resolvedForgeConfig, 'ELECTRON_FORGE');
};
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiZm9yZ2UtY29uZmlnLmpzIiwic291cmNlUm9vdCI6IiIsInNvdXJjZXMiOlsiLi4vLi4vc3JjL3V0aWwvZm9yZ2UtY29uZmlnLnRzIl0sIm5hbWVzIjpbXSwibWFwcGluZ3MiOiI7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7O0FBQUEsMERBQTZCO0FBRzdCLDZCQUEwQjtBQUMxQix3REFBMEI7QUFDMUIscURBQXVDO0FBQ3ZDLCtCQUFrQztBQUNsQyxpREFBbUM7QUFFbkMsK0NBQStDO0FBQy9DLHNFQUFvRTtBQUVwRSxpQ0FBeUM7QUFDekMsMEVBQWlEO0FBQ2pELDJEQUF5RDtBQUV6RCxNQUFNLGNBQWMsR0FBRyxDQUFDLEdBQVcsRUFBRSxFQUFFLENBQ3JDLEdBQUc7S0FDQSxPQUFPLENBQUMsbUJBQW1CLEVBQUUsT0FBTyxDQUFDO0tBQ3JDLE9BQU8sQ0FBQyxvQkFBb0IsRUFBRSxPQUFPLENBQUM7S0FDdEMsV0FBVyxFQUFFLENBQUM7QUFLbkIsdURBQXVEO0FBQ3ZELFNBQVMsdUJBQXVCLENBQzlCLEtBQVU7SUFFVixPQUFPLENBQ0wsS0FBSyxJQUFJLE9BQU8sS0FBSyxLQUFLLFFBQVEsSUFBSSxLQUFLLENBQUMsMkJBQTJCLENBQ3hFLENBQUM7QUFDSixDQUFDO0FBRUQsTUFBTSxPQUFPLEdBQUcsQ0FDZCxlQUF3QyxFQUN4QyxlQUFrQixFQUNsQixTQUFpQixFQUNkLEVBQUU7SUFDTCxJQUFJLFNBQVMsR0FBTSxFQUFTLENBQUM7SUFDN0IsSUFBSSxLQUFLLENBQUMsT0FBTyxDQUFDLGVBQWUsQ0FBQyxFQUFFLENBQUM7UUFDbkMsU0FBUyxHQUFHLEVBQVMsQ0FBQztJQUN4QixDQUFDO0lBRUQsS0FBSyxNQUFNLENBQUMsR0FBRyxFQUFFLEdBQUcsQ0FBQyxJQUFJLE1BQU0sQ0FBQyxPQUFPLENBQUMsZUFBZSxDQUFDLEVBQUUsQ0FBQztRQUN6RCxJQUNFLE9BQU8sR0FBRyxLQUFLLFFBQVE7WUFDdkIsQ0FBQyxHQUFHLENBQUMsV0FBVyxLQUFL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