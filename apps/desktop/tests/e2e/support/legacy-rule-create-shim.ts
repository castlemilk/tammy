export interface LegacyRuleCreateShimArguments {
  readonly currentHashHex: string;
  readonly expectedTypeUrl: string;
  readonly legacyHashHex: string;
  readonly stateKey: string;
}

interface ElectronMainWithIpc {
  readonly ipcMain: unknown;
}

export function installLegacyRuleCreateShimInMain(
  electron: ElectronMainWithIpc,
  { currentHashHex, expectedTypeUrl, legacyHashHex, stateKey }: LegacyRuleCreateShimArguments,
): void {
  type InvokeHandler = (event: unknown, ...args: unknown[]) => unknown;
  const ipcMain = electron.ipcMain as {
    _invokeHandlers?: Map<string, InvokeHandler>;
  } & Record<string, unknown>;
  const handlers = ipcMain._invokeHandlers;
  const channel = "conveyor:organisation";
  const original = handlers?.get(channel);
  if (!handlers || !original) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_SHIM_UNAVAILABLE");

  const fromHex = (value: string) => {
    const bytes = new Uint8Array(value.length / 2);
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = Number.parseInt(value.slice(index * 2, index * 2 + 2), 16);
    }
    return bytes;
  };
  if (!/^[0-9a-f]{64}$/u.test(currentHashHex) || !/^[0-9a-f]{64}$/u.test(legacyHashHex))
    throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_HASH_INVALID");
  const currentHash = fromHex(currentHashHex);
  const legacyHash = fromHex(legacyHashHex);

  const readVarint = (bytes: Uint8Array, cursor: { offset: number }, end: number): number => {
    let value = 0;
    let shift = 0;
    while (cursor.offset < end && shift <= 49) {
      const byte = bytes[cursor.offset];
      if (byte === undefined) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
      cursor.offset += 1;
      value += (byte & 0x7f) * 2 ** shift;
      if ((byte & 0x80) === 0) return value;
      shift += 7;
    }
    throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
  };
  const skipField = (
    bytes: Uint8Array,
    cursor: { offset: number },
    end: number,
    wireType: number,
  ) => {
    if (wireType === 0) {
      readVarint(bytes, cursor, end);
      return;
    }
    if (wireType === 1 || wireType === 5) {
      cursor.offset += wireType === 1 ? 8 : 4;
    } else if (wireType === 2) {
      const length = readVarint(bytes, cursor, end);
      cursor.offset += length;
    } else {
      throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
    }
    if (cursor.offset > end) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
  };
  const sourceHashPosition = (bytes: Uint8Array, start: number, end: number) => {
    const cursor = { offset: start };
    let matchCount = 0;
    let position = -1;
    while (cursor.offset < end) {
      const tag = readVarint(bytes, cursor, end);
      const fieldNumber = Math.floor(tag / 8);
      const wireType = tag & 7;
      if (fieldNumber === 4 && wireType === 2) {
        const length = readVarint(bytes, cursor, end);
        const hashStart = cursor.offset;
        if (length === currentHash.length && hashStart + length <= end) {
          let matches = true;
          for (let index = 0; index < length; index += 1) {
            if (bytes[hashStart + index] !== currentHash[index]) matches = false;
          }
          if (matches) {
            matchCount += 1;
            position = hashStart;
          }
        }
        cursor.offset += length;
        if (cursor.offset > end) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
      } else {
        skipField(bytes, cursor, end, wireType);
      }
    }
    if (matchCount !== 1) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_REF_NOT_EXACT");
    return position;
  };
  const locateTypedMessage = (frame: Uint8Array) => {
    const cursor = { offset: 0 };
    let typeUrl: string | undefined;
    let typeUrlCount = 0;
    let payloadStart = -1;
    let payloadEnd = -1;
    let payloadCount = 0;
    while (cursor.offset < frame.length) {
      const tag = readVarint(frame, cursor, frame.length);
      const fieldNumber = Math.floor(tag / 8);
      const wireType = tag & 7;
      if ((fieldNumber === 1 || fieldNumber === 2) && wireType !== 2)
        throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
      if (fieldNumber === 1) {
        const length = readVarint(frame, cursor, frame.length);
        const start = cursor.offset;
        const end = start + length;
        if (end > frame.length) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
        typeUrlCount += 1;
        try {
          typeUrl = new TextDecoder("utf-8", { fatal: true }).decode(frame.subarray(start, end));
        } catch {
          throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
        }
        cursor.offset = end;
      } else if (fieldNumber === 2) {
        const length = readVarint(frame, cursor, frame.length);
        payloadStart = cursor.offset;
        payloadEnd = payloadStart + length;
        if (payloadEnd > frame.length) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
        payloadCount += 1;
        cursor.offset = payloadEnd;
      } else {
        skipField(frame, cursor, frame.length, wireType);
      }
    }
    if (typeUrlCount !== 1 || payloadCount !== 1 || payloadStart < 0 || payloadEnd < 0)
      throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
    if (typeUrl !== expectedTypeUrl) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_TYPE_MISMATCH");
    return { payloadStart, payloadEnd };
  };
  const locateBundleHash = (frame: Uint8Array) => {
    const { payloadStart, payloadEnd } = locateTypedMessage(frame);
    const cursor = { offset: payloadStart };
    let bundleCount = 0;
    let hashPosition = -1;
    while (cursor.offset < payloadEnd) {
      const tag = readVarint(frame, cursor, payloadEnd);
      const fieldNumber = Math.floor(tag / 8);
      const wireType = tag & 7;
      if (fieldNumber === 9 && wireType === 2) {
        const length = readVarint(frame, cursor, payloadEnd);
        const start = cursor.offset;
        const end = start + length;
        if (end > payloadEnd) throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_FRAME_INVALID");
        bundleCount += 1;
        hashPosition = sourceHashPosition(frame, start, end);
        cursor.offset = end;
      } else {
        skipField(frame, cursor, payloadEnd, wireType);
      }
    }
    if (bundleCount !== 1 || hashPosition < 0)
      throw new Error("BAS_ACCEPTANCE_LEGACY_RULE_REF_NOT_EXACT");
    return hashPosition;
  };

  const state = { replacementCount: 0, restored: false };
  Object.defineProperty(ipcMain, stateKey, { configurable: true, value: state });
  const wrapped: InvokeHandler = async (event, ...args) => {
    if (args[0] !== "createOrganisation" || !(args[1] instanceof Uint8Array)) {
      return Reflect.apply(original, ipcMain, [event, ...args]);
    }
    if (state.replacementCount !== 0) return Reflect.apply(original, ipcMain, [event, ...args]);
    const request = args[1];
    const hashPosition = locateBundleHash(request);
    const rewritten = request.slice();
    rewritten.set(legacyHash, hashPosition);
    state.replacementCount = 1;
    handlers.set(channel, original);
    state.restored = true;
    const forwardedArgs = args.slice();
    forwardedArgs[1] = rewritten;
    return Reflect.apply(original, ipcMain, [event, ...forwardedArgs]);
  };
  handlers.set(channel, wrapped);
}
