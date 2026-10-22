import { ConveyorError } from "electron-conveyor/define";
import { isBasPreparationCorrectionCode } from "../shared/bas-preparation-errors";
import { isPurchaseCorrectionCode } from "../shared/purchase-errors";

export function sanitizeHandlerError(error: unknown): ConveyorError {
  if (
    error instanceof ConveyorError &&
    (isPurchaseCorrectionCode(error.code) || isBasPreparationCorrectionCode(error.code)) &&
    error.message === error.code
  )
    return new ConveyorError(error.code, error.code);
  return new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
}
