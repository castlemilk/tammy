// BAS calculation and frozen records contain bounded source-coverage snapshots
// that can exceed the general-purpose IPC frame size used by smaller APIs.
export const BAS_PREPARATION_RESPONSE_FRAME_LIMIT = 16_777_216;
