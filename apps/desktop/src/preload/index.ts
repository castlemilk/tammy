import { contextBridge, ipcRenderer } from "electron";
import { parseLaunchScenarioArgument } from "../shared/launch-scenario";
import { createRestrictedConveyorBridge, exposeRestrictedConveyorBridge } from "./conveyor-bridge";

exposeRestrictedConveyorBridge(contextBridge, createRestrictedConveyorBridge(ipcRenderer));
contextBridge.exposeInMainWorld("tammyLaunchScenario", parseLaunchScenarioArgument(process.argv));
