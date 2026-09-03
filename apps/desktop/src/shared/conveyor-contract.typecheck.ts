import "electron-conveyor/renderer";
import "./conveyor-contract";

const bridge: Window["conveyor"] = window.conveyor;

void bridge;
