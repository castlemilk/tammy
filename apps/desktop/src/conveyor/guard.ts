import { ConveyorError } from "electron-conveyor/define";
import { isAllowedApplicationURL } from "../main/security";

interface SenderFrame {
  readonly frameTreeNodeId?: number;
  readonly isDestroyed: () => boolean;
  readonly processId?: number;
  readonly routingId?: number;
  readonly url: string;
}
interface SenderContents { readonly isDestroyed: () => boolean; readonly mainFrame: SenderFrame; }
interface SenderWindow { readonly isDestroyed: () => boolean; readonly webContents: SenderContents; }
interface SenderEvent { readonly sender: SenderContents; readonly senderFrame: SenderFrame | null; }

function isRegisteredMainFrame(frame: SenderFrame, mainFrame: SenderFrame): boolean {
  if (frame === mainFrame) return true;
  if (
    Number.isSafeInteger(frame.frameTreeNodeId) &&
    Number.isSafeInteger(mainFrame.frameTreeNodeId)
  ) {
    return frame.frameTreeNodeId === mainFrame.frameTreeNodeId;
  }
  return (
    Number.isSafeInteger(frame.processId) &&
    Number.isSafeInteger(frame.routingId) &&
    Number.isSafeInteger(mainFrame.processId) &&
    Number.isSafeInteger(mainFrame.routingId) &&
    frame.processId === mainFrame.processId &&
    frame.routingId === mainFrame.routingId
  );
}

export function createConveyorAuthorizer(options: Readonly<{ applicationUrl: string; getWindow: () => SenderWindow | undefined }>): (event: SenderEvent) => void {
  return (event) => {
    try {
      const window = options.getWindow();
      if (window === undefined || window.isDestroyed()) throw new Error("REJECTED");
      const contents = window.webContents;
      if (contents === undefined || contents.isDestroyed() || event.sender !== contents) throw new Error("REJECTED");
      const senderFrame = event.senderFrame;
      if (
        senderFrame === null ||
        !isRegisteredMainFrame(senderFrame, contents.mainFrame) ||
        senderFrame.isDestroyed() ||
        !isAllowedApplicationURL(senderFrame.url, options.applicationUrl)
      ) {
        throw new Error("REJECTED");
      }
    } catch {
      throw new ConveyorError("IPC_SENDER_REJECTED", "IPC_SENDER_REJECTED");
    }
  };
}
