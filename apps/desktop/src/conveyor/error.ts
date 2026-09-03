import { ConveyorError } from "electron-conveyor/define";

export function sanitizeHandlerError(_error: unknown): ConveyorError {
  return new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
}
