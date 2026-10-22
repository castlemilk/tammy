"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.initNPM = exports.exactDevDeps = exports.devDeps = exports.deps = exports.siblingDep = void 0;
const node_path_1 = __importDefault(require("node:path"));
const debug_1 = __importDefault(require("debug"));
const fs_extra_1 = __importDefault(require("fs-extra"));
const semver_1 = __importDefault(require("semver"));
const install_dependencies_1 = require("../../util/install-dependencies");
const d = (0, debug_1.default)('electron-forge:init:npm');
const corePackage = fs_extra_1.default.readJsonSync(node_path_1.default.resolve(__dirname, '../../../package.json'));
function siblingDep(name) {
    return `@electron-forge/${name}@^${corePackage.version}`;
}
exports.siblingDep = siblingDep;
exports.deps = ['electron-squirrel-startup'];
exports.devDeps = [
    '@electron/fuses@^1.0.0',
    siblingDep('cli'),
    siblingDep('maker-squirrel'),
    siblingDep('maker-zip'),
    siblingDep('maker-deb'),
    siblingDep('maker-rpm'),
    siblingDep('plugin-auto-unpack-natives'),
    siblingDep('plugin-fuses'),
];
exports.exactDevDeps = ['electron'];
const initNPM = async (pm, dir, electronVersion, task) => {
    d('installing dependencies');
    task.output = `${pm.executable} ${pm.install} ${exports.deps.join(' ')}`;
    await (0, install_dependencies_1.installDependencies)(pm, dir, exports.deps);
    d(`installing devDependencies`);
    task.output = `${pm.executable} ${pm.install} ${pm.dev} ${exports.devDeps.join(' ')}`;
    await (0, install_dependencies_1.installDependencies)(pm, dir, exports.devDeps, install_dependencies_1.DepType.DEV);
    d('installing exact devDependencies');
    for (const packageName of exports.exactDevDeps) {
        let packageInstallString = packageName;
        if (packageName === 'electron') {
            if (electronVersion === 'nightly') {
                packageInstallString = `electron-nightly@latest`;
            }
            else if (semver_1.default.prerelease(electronVersion)?.includes('nightly')) {
                packageInstallString = `electron-nightly@${electronVersion}`;
            }
            else {
                packageInstallString += `@${electronVersion}`;
            }
        }
        task.output = `${pm.executable} ${pm.install} ${pm.dev} ${pm.exact} ${packageInstallString}`;
        await (0, install_dependencies_1.installDependencies)(pm, dir, [packageInstallString], install_dependencies_1.DepType.DEV, install_dependencies_1.DepVersionRestriction.EXACT);
    }
};
exports.initNPM = initNPM;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiaW5pdC1ucG0uanMiLCJzb3VyY2VSb290IjoiIiwic291cmNlcyI6WyIuLi8uLi8uLi9zcmMvYXBpL2luaXQtc2NyaXB0cy9pbml0LW5wbS50cyJdLCJuYW1lcyI6W10sIm1hcHBpbmdzIjoiOzs7Ozs7QUFBQSwwREFBNkI7QUFJN0Isa0RBQTBCO0FBQzFCLHdEQUEwQjtBQUMxQixvREFBNEI7QUFFNUIsMEVBSXlDO0FBRXpDLE1BQU0sQ0FBQyxHQUFHLElBQUEsZUFBSyxFQUFDLHlCQUF5QixDQUFDLENBQUM7QUFDM0MsTUFBTSxXQUFXLEdBQUcsa0JBQUUsQ0FBQyxZQUFZLENBQ2pDLG1CQUFJLENBQUMsT0FBTyxDQUFDLFNBQVMsRUFBRSx1QkFBdUIsQ0FBQyxDQUNqRCxDQUFDO0FBRUYsU0FBZ0IsVUFBVSxDQUFDLElBQVk7SUFDckMsT0FBTyxtQkFBbUIsSUFBSSxLQUFLLFdBQVcsQ0FBQyxPQUFPLEVBQUUsQ0FBQztBQUMzRCxDQUFDO0FBRkQsZ0NBRUM7QUFFWSxRQUFBLElBQUksR0FBRyxDQUFDLDJCQUEyQixDQUFDLENBQUM7QUFDckMsUUFBQSxPQUFPLEdBQUc7SUFDckIsd0JBQXdCO0lBQ3hCLFVBQVUsQ0FBQyxLQUFLLENBQUM7SUFDakIsVUFBVSxDQUFDLGdCQUFnQixDQUFDO0lBQzVCLFVBQVUsQ0FBQyxXQUFXLENBQUM7SUFDdkIsVUFBVSxDQUFDLFdBQVcsQ0FBQztJQUN2QixVQUFVLENBQUMsV0FBVyxDQUFDO0lBQ3ZCLFVBQVUsQ0FBQyw0QkFBNEIsQ0FBQztJQUN4QyxVQUFVLENBQUMsY0FBYyxDQUFDO0NBQzNCLENBQUM7QUFDVyxRQUFBLFlBQVksR0FBRyxDQUFDLFVBQVUsQ0FBQyxDQUFDO0FBRWxDLE1BQU0sT0FBTyxHQUFHLEtBQUssRUFDMUIsRUFBYSxFQUNiLEdBQVcsRUFDWCxlQUF1QixFQUN2QixJQUF1QixFQUNSLEVBQUU7SUFDakIsQ0FBQyxDQUFDLHlCQUF5QixDQUFDLENBQUM7SUFDN0IsSUFBSSxDQUFDLE1BQU0sR0FBRyxHQUFHLEVBQUUsQ0FBQyxVQUFVLElBQUksRUFBRSxDQUFDLE9BQU8sSUFBSSxZQUFJLENBQUMsSUFBSSxDQUFDLEdBQUcsQ0FBQyxFQUFFLENBQUM7SUFDakUsTUFBTSxJQUFBLDBDQUFtQixFQUFDLEVBQUUsRUFBRSxHQUFHLEVBQUUsWUFBSSxDQUFDLENBQUM7SUFFekMsQ0FBQyxDQUFDLDRCQUE0QixDQUFDLENBQUM7SUFDaEMsSUFBSSxDQUFDLE1BQU0sR0FBRyxHQUFHLEVBQUUsQ0FBQyxVQUFVLElBQUksRUFBRSxDQUFDLE9BQU8sSUFBSSxFQUFFLENBQUMsR0FBRyxJQUFJLGVBQU8sQ0FBQyxJQUFJLENBQUMsR0FBRyxDQUFDLEVBQUUsQ0FBQztJQUM5RSxNQUFNLElBQUEsMENBQW1CLEVBQUMsRUFBRSxFQUFFLEdBQUcsRUFBRSxlQUFPLEVBQUUsOEJBQU8sQ0FBQyxHQUFHLENBQUMsQ0FBQztJQUV6RCxDQUFDLENBQUMsa0NBQWtDLENBQUMsQ0FBQztJQUN0QyxLQUFLLE1BQU0sV0FBVyxJQUFJLG9CQUFZLEVBQUUsQ0FBQztRQUN2QyxJQUFJLG9CQUFvQixHQUFHLFdBQVcsQ0FBQztRQUN2QyxJQUFJLFdBQVcsS0FBSyxVQUFVLEVBQUUsQ0FBQztZQUMvQixJQUFJLGVBQWUsS0FBSyxTQUFTLEVBQUUsQ0FBQztnQkFDbEMsb0JBQW9CLEdBQUcseUJBQXlCLENBQUM7WUFDbkQsQ0FBQztpQkFBTSxJQUFJLGdCQUFNLENBQUMsVUFBVSxDQUFDLGVBQWUsQ0FBQyxFQUFFLFFBQVEsQ0FBQyxTQUFTLENBQUMsRUFBRSxDQUFDO2dCQUNuRSxvQkFBb0IsR0FBRyxvQkFBb0IsZUFBZSxFQUFFLENBQUM7WUFDL0QsQ0FBQztpQkFBTSxDQUFDO2dCQUNOLG9CQUFvQixJQUFJLElBQUksZUFBZSxFQUFFLENBQUM7WUFDaEQsQ0FBQztRQUNILENBQUM7UUFDRCxJQUFJLENBQUMsTUFBTSxHQUFHLEdBQUcsRUFBRSxDQUFDLFVBQVUsSUFBSSxFQUFFLENBQUMsT0FBTyxJQUFJLEVBQUUsQ0FBQyxHQUFHLElBQUksRUFBRSxDQUFDLEtBQUssSUFBSSxvQkFBb0IsRUFBRSxDQUFDO1FBQzdGLE1BQU0sSUFBQSwwQ0FBbUIsRUFDdkIsRUFBRSxFQUNGLEdBQUcsRUFDSCxDQUFDLG9CQUFvQixDQUFDLEVBQ3RCLDhCQUFPLENBQUMsR0FBRyxFQUNYLDRDQUFxQixDQUFDLEtBQUssQ0FDNUIsQ0FBQztJQUNKLENBQUM7QUFDSCxDQUFDLENBQUM7QUFuQ1csUUFBQSxPQUFPLFdBbUNsQiJ9