import "electron-conveyor/renderer";
import type { CONVEYOR_PROCEDURE_CHANNELS } from "./conveyor-contract";

const bridge: Window["conveyor"] = window.conveyor;

const purchaseChannel: (typeof CONVEYOR_PROCEDURE_CHANNELS)[number] = "conveyor:purchases";

void bridge;
void purchaseChannel;
