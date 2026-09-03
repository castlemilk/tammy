import { ConveyorError } from "electron-conveyor/define";
import { isAllowedApplicationURL } from "../main/security";

interface SenderFrame { readonly isDestroyed: () => boolean; readonly url: string; }
interface SenderContents { readonly isDestroyed: () => boolean; readonly mainFrame: SenderFrame; }
interface SenderWindow { readonly isDestroyed: () => boolean; readonly webContents: SenderContents; }
interface SenderEvent { readonly sender: SenderContents; readonly senderFrame: SenderFrame | null; }

export function createConveyorAuthorizer(options: Readonly<{ applicationUrl: string; getWindow: () => SenderWindow | undefined }>): (event: SenderEvent) => void {
  return (event) => {
    try {
      const window = options.getWindow();
      if (window === undefined || window.isDestroyed()) throw new Error("REJECTED");
      const contents = window.webContents;
      if (contents === undefined || contents.isDestroyed() || event.sender !== contents) throw new Error("REJECTED");
      const senderFrame = event.senderFrame;
      if (senderFrame === null || senderFrame !== contents.mainFrame || senderFrame.isDestroyed() || !isAllowedApplicationURL(senderFrame.url, options.applicationUrl)) throw new Error("REJECTED");
    } catch {
      throw new ConveyorError("IPC_SENDER_REJECTED", "IPC_SENDER_REJECTED");
    }
  };
}
