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
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
var __generator = (this && this.__generator) || function (thisArg, body) {
    var _ = { label: 0, sent: function() { if (t[0] & 1) throw t[1]; return t[1]; }, trys: [], ops: [] }, f, y, t, g;
    return g = { next: verb(0), "throw": verb(1), "return": verb(2) }, typeof Symbol === "function" && (g[Symbol.iterator] = function() { return this; }), g;
    function verb(n) { return function (v) { return step([n, v]); }; }
    function step(op) {
        if (f) throw new TypeError("Generator is already executing.");
        while (g && (g = 0, op[0] && (_ = 0)), _) try {
            if (f = 1, y && (t = op[0] & 2 ? y["return"] : op[0] ? y["throw"] || ((t = y["return"]) && t.call(y), 0) : y.next) && !(t = t.call(y, op[1])).done) return t;
            if (y = 0, t) op = [op[0] & 2, t.value];
            switch (op[0]) {
                case 0: case 1: t = op; break;
                case 4: _.label++; return { value: op[1], done: false };
                case 5: _.label++; y = op[1]; op = [0]; continue;
                case 7: op = _.ops.pop(); _.trys.pop(); continue;
                default:
                    if (!(t = _.trys, t = t.length > 0 && t[t.length - 1]) && (op[0] === 6 || op[0] === 2)) { _ = 0; continue; }
                    if (op[0] === 3 && (!t || (op[1] > t[0] && op[1] < t[3]))) { _.label = op[1]; break; }
                    if (op[0] === 6 && _.label < t[1]) { _.label = t[1]; t = op; break; }
                    if (t && _.label < t[2]) { _.label = t[2]; _.ops.push(op); break; }
                    if (t[2]) _.ops.pop();
                    _.trys.pop(); continue;
            }
            op = body.call(thisArg, _);
        } catch (e) { op = [6, e]; y = 0; } finally { f = t = 0; }
        if (op[0] & 5) throw op[1]; return { value: op[0] ? op[1] : void 0, done: true };
    }
};
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.createWindowsInstaller = exports.sanitizeAuthors = exports.convertVersion = void 0;
var asar = __importStar(require("@electron/asar"));
var temp_utils_1 = require("./temp-utils");
var fs = __importStar(require("fs-extra"));
var path = __importStar(require("path"));
var os = __importStar(require("os"));
var child_process_1 = require("child_process");
var spawn_promise_1 = __importDefault(require("./spawn-promise"));
var lodash_1 = require("lodash");
var sign_1 = require("./sign");
var log = require('debug')('electron-windows-installer:main');
/**
 * A utility function to convert SemVer version strings into NuGet-compatible
 * version strings.
 * @param version A SemVer version string
 * @returns A NuGet-compatible version string
 * @see {@link https://semver.org/ | Semantic Versioning specification}
 * @see {@link https://learn.microsoft.com/en-us/nuget/concepts/package-versioning?tabs=semver20sort | NuGet versioning specification}
 */
function convertVersion(version) {
    var parts = version.split('+')[0].split('-');
    var mainVersion = parts.shift();
    if (parts.length > 0) {
        return [mainVersion, parts.join('-').replace(/\./g, '')].join('-');
    }
    else {
        return mainVersion;
    }
}
exports.convertVersion = convertVersion;
/**
 * Strips characters that NuGet does not allow in the nuspec `<authors>`
 * element. NuGet rejects an `@` character (for example, when the author is an
 * email address) with "The '@' character ... cannot be included in a name",
 * causing the build to fail.
 *
 * @param authors The raw authors value from the package metadata
 * @returns The authors value with disallowed characters removed
 * @see {@link https://github.com/electron/windows-installer/issues/389}
 */
function sanitizeAuthors(authors) {
    return authors.replace(/@/g, '');
}
exports.sanitizeAuthors = sanitizeAuthors;
function checkIfCommandExists(command) {
    var checkCommand = os.platform() === 'win32' ? 'where' : 'which';
    return new Promise(function (resolve) {
        (0, child_process_1.exec)("".concat(checkCommand, " ").concat(command), function (error) {
            resolve(error ? false : true);
        });
    });
}
/**
 * This package's main function, which creates a Squirrel.Windows executable
 * installer and optionally code-signs the output.
 *
 * @param options Options for installer generation and signing
 * @see {@link https://github.com/Squirrel/Squirrel.Windows | Squirrel.Windows}
 */
function createWindowsInstaller(options) {
    return __awaiter(this, void 0, void 0, function () {
        var useMono, monoExe, wineExe, hasWine, hasMono, appDirectory, outputDirectory, loadingGif, vendorPath, vendorUpdate, appUpdate, cmd_1, args_1, defaultLoadingGif, certificateFile, certificatePassword, remoteReleases, signWithParams, remoteToken, windowsSign, metadata, appResources, asarFile, appMetadata, templatePath, templateData, _i, _a, f, nuspecContent, nugetOutput, targetNuspecPath, cmd, args, _b, nupkgPath, _c, _d, setupPath, unfixedSetupPath, msiPath, unfixedMsiPath;
        return __generator(this, function (_e) {
            switch (_e.label) {
                case 0:
                    useMono = false;
                    monoExe = 'mono';
                    wineExe = ['arm64', 'x64'].includes(process.arch) ? 'wine64' : 'wine';
                    if (!(process.platform !== 'win32')) return [3 /*break*/, 5];
                    useMono = true;
                    return [4 /*yield*/, checkIfCommandExists(wineExe)];
                case 1:
                    hasWine = _e.sent();
                    if (!(!hasWine && wineExe !== 'wine')) return [3 /*break*/, 3];
                    wineExe = 'wine';
                    return [4 /*yield*/, checkIfCommandExists(wineExe)];
                case 2:
                    hasWine = _e.sent();
                    _e.label = 3;
                case 3: return [4 /*yield*/, checkIfCommandExists(monoExe)];
                case 4:
                    hasMono = _e.sent();
                    if (!hasWine || !hasMono) {
                        throw new Error('You must install both Mono and Wine on non-Windows');
                    }
                    log("Using Mono: '".concat(monoExe, "'"));
                    log("Using Wine: '".concat(wineExe, "'"));
                    _e.label = 5;
                case 5:
                    appDirectory = options.appDirectory, outputDirectory = options.outputDirectory, loadingGif = options.loadingGif;
                    outputDirectory = path.resolve(outputDirectory || 'installer');
                    vendorPath = options.vendorDirectory || path.join(__dirname, '..', 'vendor');
                    vendorUpdate = path.join(vendorPath, 'Squirrel.exe');
                    appUpdate = path.join(appDirectory, 'Squirrel.exe');
                    return [4 /*yield*/, fs.copy(vendorUpdate, appUpdate)];
                case 6:
                    _e.sent();
                    if (!(options.setupIcon && (options.skipUpdateIcon !== true))) return [3 /*break*/, 8];
                    cmd_1 = path.join(vendorPath, 'rcedit.exe');
                    args_1 = [
                        appUpdate,
                        '--set-icon', options.setupIcon
                    ];
                    if (useMono) {
                        args_1.unshift(cmd_1);
                        cmd_1 = wineExe;
                    }
                    return [4 /*yield*/, (0, spawn_promise_1.default)(cmd_1, args_1)];
                case 7:
                    _e.sent();
                    _e.label = 8;
                case 8:
                    defaultLoadingGif = path.join(__dirname, '..', 'resources', 'install-spinner.gif');
                    loadingGif = loadingGif ? path.resolve(loadingGif) : defaultLoadingGif;
                    certificateFile = options.certificateFile, certificatePassword = options.certificatePassword, remoteReleases = options.remoteReleases, signWithParams = options.signWithParams, remoteToken = options.remoteToken, windowsSign = options.windowsSign;
                    metadata = {
                        description: '',
                        iconUrl: 'https://raw.githubusercontent.com/electron/electron/main/shell/browser/resources/win/electron.ico'
                    };
                    if (!(options.usePackageJson !== false)) return [3 /*break*/, 13];
                    appResources = path.join(appDirectory, 'resources');
                    asarFile = path.join(appResources, 'app.asar');
                    appMetadata = void 0;
                    return [4 /*yield*/, fs.pathExists(asarFile)];
                case 9:
                    if (!_e.sent()) return [3 /*break*/, 10];
                    appMetadata = JSON.parse(asar.extractFile(asarFile, 'package.json').toString());
                    return [3 /*break*/, 12];
                case 10: return [4 /*yield*/, fs.readJson(path.join(appResources, 'app', 'package.json'))];
                case 11:
                    appMetadata = _e.sent();
                    _e.label = 12;
                case 12:
                    Object.assign(metadata, {
                        exe: "".concat(appMetadata.name, ".exe"),
                        title: appMetadata.productName || appMetadata.name
                    }, appMetadata);
                    _e.label = 13;
                case 13:
                    Object.assign(metadata, options);
                    if (!metadata.authors) {
                        if (typeof (metadata.author) === 'string') {
                            metadata.authors = metadata.author;
                        }
                        else {
                            metadata.authors = (metadata.author || {}).name || '';
                        }
                    }
                    metadata.owners = metadata.owners || metadata.authors;
                    metadata.version = convertVersion(metadata.version);
                    metadata.copyright = metadata.copyright ||
                        "Copyright \u00A9 ".concat(new Date().getFullYear(), " ").concat(metadata.authors || metadata.owners);
                    metadata.additionalFiles = metadata.additionalFiles || [];
                    // NuGet does not allow an `@` in the nuspec `<authors>` element, so strip it
                    // out. This must happen after `owners` and `copyright` default to `authors`,
                    // since those fields are not affected by the same restriction.
                    // See https://github.com/electron/windows-installer/issues/389
                    metadata.authors = sanitizeAuthors(metadata.authors);
                    return [4 /*yield*/, fs.pathExists(path.join(appDirectory, 'swiftshader'))];
                case 14:
                    if (_e.sent()) {
                        metadata.additionalFiles.push({ src: 'swiftshader\\**', target: 'lib\\net45\\swiftshader' });
                    }
                    return [4 /*yield*/, fs.pathExists(path.join(appDirectory, 'vk_swiftshader_icd.json'))];
                case 15:
                    if (_e.sent()) {
                        metadata.additionalFiles.push({ src: 'vk_swiftshader_icd.json', target: 'lib\\net45' });
                    }
                    templatePath = options.nuspecTemplate || path.join(__dirname, '..', 'template.nuspectemplate');
                    return [4 /*yield*/, fs.readFile(templatePath, 'utf8')];
                case 16:
                    templateData = _e.sent();
                    if (path.sep === '/') {
                        templateData = templateData.replace(/\\/g, '/');
                        for (_i = 0, _a = metadata.additionalFiles; _i < _a.length; _i++) {
                            f = _a[_i];
                            f.src = f.src.replace(/\\/g, '/');
                            f.target = f.target.replace(/\\/g, '/');
                        }
                    }
                    nuspecContent = (0, lodash_1.template)(templateData)(metadata);
                    log("Created NuSpec file:\n".concat(nuspecContent));
                    return [4 /*yield*/, (0, temp_utils_1.createTempDir)('si-')];
                case 17:
                    nugetOutput = _e.sent();
                    targetNuspecPath = path.join(nugetOutput, metadata.name + '.nuspec');
                    return [4 /*yield*/, fs.writeFile(targetNuspecPath, nuspecContent)];
                case 18:
                    _e.sent();
                    cmd = path.join(vendorPath, 'nuget.exe');
                    args = [
                        'pack', targetNuspecPath,
                        '-BasePath', appDirectory,
                        '-OutputDirectory', nugetOutput,
                        '-NoDefaultExcludes'
                    ];
                    if (useMono) {
                        args.unshift(cmd);
                        cmd = monoExe;
                    }
                    // Call NuGet to create our package
                    _b = log;
                    return [4 /*yield*/, (0, spawn_promise_1.default)(cmd, args)];
                case 19:
                    // Call NuGet to create our package
                    _b.apply(void 0, [_e.sent()]);
                    nupkgPath = path.join(nugetOutput, "".concat(metadata.name, ".").concat(metadata.version, ".nupkg"));
                    if (!remoteReleases) return [3 /*break*/, 21];
                    cmd = path.join(vendorPath, 'SyncReleases.exe');
                    args = ['-u', remoteReleases, '-r', outputDirectory];
                    if (useMono) {
                        args.unshift(cmd);
                        cmd = monoExe;
                    }
                    if (remoteToken) {
                        args.push('-t', remoteToken);
                    }
                    _c = log;
                    return [4 /*yield*/, (0, spawn_promise_1.default)(cmd, args)];
                case 20:
                    _c.apply(void 0, [_e.sent()]);
                    _e.label = 21;
                case 21:
                    cmd = path.join(vendorPath, 'Squirrel.exe');
                    args = [
                        '--releasify', nupkgPath,
                        '--releaseDir', outputDirectory,
                        '--loadingGif', loadingGif
                    ];
                    if (useMono) {
                        args.unshift(path.join(vendorPath, 'Squirrel-Mono.exe'));
                        cmd = monoExe;
                    }
                    // Legacy codesign options
                    return [4 /*yield*/, (0, sign_1.resetSignTool)()];
                case 22:
                    // Legacy codesign options
                    _e.sent();
                    if (!signWithParams) return [3 /*break*/, 23];
                    args.push('--signWithParams');
                    if (!signWithParams.includes('/f') && !signWithParams.includes('/p') && certificateFile && certificatePassword) {
                        args.push("".concat(signWithParams, " /a /f \"").concat(path.resolve(certificateFile), "\" /p \"").concat(certificatePassword, "\""));
                    }
                    else {
                        args.push(signWithParams);
                    }
                    return [3 /*break*/, 26];
                case 23:
                    if (!(certificateFile && certificatePassword)) return [3 /*break*/, 24];
                    args.push('--signWithParams');
                    args.push("/a /f \"".concat(path.resolve(certificateFile), "\" /p \"").concat(certificatePassword, "\""));
                    return [3 /*break*/, 26];
                case 24:
                    if (!windowsSign) return [3 /*break*/, 26];
                    args.push('--signWithParams');
                    args.push('windows-sign');
                    return [4 /*yield*/, (0, sign_1.createSignTool)(options)];
                case 25:
                    _e.sent();
                    _e.label = 26;
                case 26:
                    if (options.setupIcon) {
                        args.push('--setupIcon');
                        args.push(path.resolve(options.setupIcon));
                    }
                    if (options.noMsi) {
                        args.push('--no-msi');
                    }
                    if (options.noDelta) {
                        args.push('--no-delta');
                    }
                    if (options.frameworkVersion) {
                        args.push('--framework-version');
                        args.push(options.frameworkVersion);
                    }
                    _d = log;
                    return [4 /*yield*/, (0, spawn_promise_1.default)(cmd, args)];
                case 27:
                    _d.apply(void 0, [_e.sent()]);
                    if (!(options.fixUpPaths !== false)) return [3 /*break*/, 32];
                    log('Fixing up paths');
                    if (!(metadata.productName || options.setupExe)) return [3 /*break*/, 29];
                    setupPath = path.join(outputDirectory, options.setupExe || "".concat(metadata.productName, "Setup.exe"));
                    unfixedSetupPath = path.join(outputDirectory, 'Setup.exe');
                    log("Renaming ".concat(unfixedSetupPath, " => ").concat(setupPath));
                    return [4 /*yield*/, fs.rename(unfixedSetupPath, setupPath)];
                case 28:
                    _e.sent();
                    _e.label = 29;
                case 29:
                    if (!(metadata.productName || options.setupMsi)) return [3 /*break*/, 32];
                    msiPath = path.join(outputDirectory, options.setupMsi || "".concat(metadata.productName, "Setup.msi"));
                    unfixedMsiPath = path.join(outputDirectory, 'Setup.msi');
                    return [4 /*yield*/, fs.pathExists(unfixedMsiPath)];
                case 30:
                    if (!_e.sent()) return [3 /*break*/, 32];
                    log("Renaming ".concat(unfixedMsiPath, " => ").concat(msiPath));
                    return [4 /*yield*/, fs.rename(unfixedMsiPath, msiPath)];
                case 31:
                    _e.sent();
                    _e.label = 32;
                case 32: return [4 /*yield*/, (0, sign_1.resetSignTool)()];
                case 33:
                    _e.sent();
                    return [2 /*return*/];
            }
        });
    });
}
exports.createWindowsInstaller = createWindowsInstaller;
//# sourceMappingURL=index.js.map