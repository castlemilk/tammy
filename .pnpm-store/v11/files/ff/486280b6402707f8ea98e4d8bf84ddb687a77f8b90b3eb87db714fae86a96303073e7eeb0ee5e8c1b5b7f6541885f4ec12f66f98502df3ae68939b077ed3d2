"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.installDependencies = exports.DepVersionRestriction = exports.DepType = void 0;
const core_utils_1 = require("@electron-forge/core-utils");
const cross_spawn_promise_1 = require("@malept/cross-spawn-promise");
const debug_1 = __importDefault(require("debug"));
const d = (0, debug_1.default)('electron-forge:dependency-installer');
var DepType;
(function (DepType) {
    DepType["PROD"] = "PROD";
    DepType["DEV"] = "DEV";
})(DepType || (exports.DepType = DepType = {}));
var DepVersionRestriction;
(function (DepVersionRestriction) {
    DepVersionRestriction["EXACT"] = "EXACT";
    DepVersionRestriction["RANGE"] = "RANGE";
})(DepVersionRestriction || (exports.DepVersionRestriction = DepVersionRestriction = {}));
async function installDependencies(pm, dir, deps, depType = DepType.PROD, versionRestriction = DepVersionRestriction.RANGE) {
    d('installing', JSON.stringify(deps), 'in:', dir, `depType=${depType},versionRestriction=${versionRestriction},withPackageManager=${pm.executable}`);
    if (deps.length === 0) {
        d('nothing to install, stopping immediately');
        return Promise.resolve();
    }
    const cmd = [pm.install].concat(deps);
    if (depType === DepType.DEV)
        cmd.push(pm.dev);
    if (versionRestriction === DepVersionRestriction.EXACT)
        cmd.push(pm.exact);
    d('executing', JSON.stringify(cmd), 'in:', dir);
    try {
        await (0, core_utils_1.spawnPackageManager)(pm, cmd, {
            cwd: dir,
            stdio: 'pipe',
        });
    }
    catch (err) {
        if (err instanceof cross_spawn_promise_1.ExitError) {
            throw new Error(`Failed to install modules: ${JSON.stringify(deps)}\n\nWith output: ${err.message}\n${err.stderr ? err.stderr.toString() : ''}`);
        }
        else {
            throw err;
        }
    }
}
exports.installDependencies = installDependencies;
//# sourceMappingURL=data:application/json;base64,eyJ2ZXJzaW9uIjozLCJmaWxlIjoiaW5zdGFsbC1kZXBlbmRlbmNpZXMuanMiLCJzb3VyY2VSb290IjoiIiwic291cmNlcyI6WyIuLi8uLi9zcmMvdXRpbC9pbnN0YWxsLWRlcGVuZGVuY2llcy50cyJdLCJuYW1lcyI6W10sIm1hcHBpbmdzIjoiOzs7Ozs7QUFBQSwyREFBNEU7QUFDNUUscUVBQXdEO0FBQ3hELGtEQUEwQjtBQUUxQixNQUFNLENBQUMsR0FBRyxJQUFBLGVBQUssRUFBQyxxQ0FBcUMsQ0FBQyxDQUFDO0FBRXZELElBQVksT0FHWDtBQUhELFdBQVksT0FBTztJQUNqQix3QkFBYSxDQUFBO0lBQ2Isc0JBQVcsQ0FBQTtBQUNiLENBQUMsRUFIVyxPQUFPLHVCQUFQLE9BQU8sUUFHbEI7QUFFRCxJQUFZLHFCQUdYO0FBSEQsV0FBWSxxQkFBcUI7SUFDL0Isd0NBQWUsQ0FBQTtJQUNmLHdDQUFlLENBQUE7QUFDakIsQ0FBQyxFQUhXLHFCQUFxQixxQ0FBckIscUJBQXFCLFFBR2hDO0FBRU0sS0FBSyxVQUFVLG1CQUFtQixDQUN2QyxFQUFhLEVBQ2IsR0FBVyxFQUNYLElBQWMsRUFDZCxPQUFPLEdBQUcsT0FBTyxDQUFDLElBQUksRUFDdEIsa0JBQWtCLEdBQUcscUJBQXFCLENBQUMsS0FBSztJQUVoRCxDQUFDLENBQ0MsWUFBWSxFQUNaLElBQUksQ0FBQyxTQUFTLENBQUMsSUFBSSxDQUFDLEVBQ3BCLEtBQUssRUFDTCxHQUFHLEVBQ0gsV0FBVyxPQUFPLHVCQUF1QixrQkFBa0IsdUJBQXVCLEVBQUUsQ0FBQyxVQUFVLEVBQUUsQ0FDbEcsQ0FBQztJQUNGLElBQUksSUFBSSxDQUFDLE1BQU0sS0FBSyxDQUFDLEVBQUUsQ0FBQztRQUN0QixDQUFDLENBQUMsMENBQTBDLENBQUMsQ0FBQztRQUM5QyxPQUFPLE9BQU8sQ0FBQyxPQUFPLEVBQUUsQ0FBQztJQUMzQixDQUFDO0lBQ0QsTUFBTSxHQUFHLEdBQUcsQ0FBQyxFQUFFLENBQUMsT0FBTyxDQUFDLENBQUMsTUFBTSxDQUFDLElBQUksQ0FBQyxDQUFDO0lBQ3RDLElBQUksT0FBTyxLQUFLLE9BQU8sQ0FBQyxHQUFHO1FBQUUsR0FBRyxDQUFDLElBQUksQ0FBQyxFQUFFLENBQUMsR0FBRyxDQUFDLENBQUM7SUFDOUMsSUFBSSxrQkFBa0IsS0FBSyxxQkFBcUIsQ0FBQyxLQUFLO1FBQUUsR0FBRyxDQUFDLElBQUksQ0FBQyxFQUFFLENBQUMsS0FBSyxDQUFDLENBQUM7SUFFM0UsQ0FBQyxDQUFDLFdBQVcsRUFBRSxJQUFJLENBQUMsU0FBUyxDQUFDLEdBQUcsQ0FBQyxFQUFFLEtBQUssRUFBRSxHQUFHLENBQUMsQ0FBQztJQUNoRCxJQUFJLENBQUM7UUFDSCxNQUFNLElBQUEsZ0NBQW1CLEVBQUMsRUFBRSxFQUFFLEdBQUcsRUFBRTtZQUNqQyxHQUFHLEVBQUUsR0FBRztZQUNSLEtBQUssRUFBRSxNQUFNO1NBQ2QsQ0FBQyxDQUFDO0lBQ0wsQ0FBQztJQUFDLE9BQU8sR0FBRyxFQUFFLENBQUM7UUFDYixJQUFJLEdBQUcsWUFBWSwrQkFBUyxFQUFFLENBQUM7WUFDN0IsTUFBTSxJQUFJLEtBQUssQ0FDYiw4QkFBOEIsSUFBSSxDQUFDLFNBQVMsQ0FBQyxJQUFJLENBQUMsb0JBQW9CLEdBQUcsQ0FBQyxPQUFPLEtBQUssR0FBRyxDQUFDLE1BQU0sQ0FBQyxDQUFDLENBQUMsR0FBRyxDQUFDLE1BQU0sQ0FBQyxRQUFRLEVBQUUsQ0FBQyxDQUFDLENBQUMsRUFBRSxFQUFFLENBQ2hJLENBQUM7UUFDSixDQUFDO2FBQU0sQ0FBQztZQUNOLE1BQU0sR0FBRyxDQUFDO1FBQ1osQ0FBQztJQUNILENBQUM7QUFDSCxDQUFDO0FBckNELGtEQXFDQyJ9