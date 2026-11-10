export function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = milliseconds & 0xff;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function today(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

export function displayDate(value: string | null | undefined): string {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return "—";
  const [year, month, day] = value.split("-");
  return `${day}/${month}/${year}`;
}

/** A deadline in local time, with the day when it is not today. */
export function deadlineLabel(value: string, now = new Date()): string {
  const deadline = new Date(value);
  if (Number.isNaN(deadline.getTime())) return "";
  const time = deadline.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  return deadline.toDateString() === now.toDateString()
    ? time
    : `${deadline.toLocaleDateString(undefined, { day: "numeric", month: "short" })}, ${time}`;
}

export function errorMessage(error: unknown): string {
  return error instanceof Error && error.message
    ? error.message
    : "The local service could not be reached.";
}
