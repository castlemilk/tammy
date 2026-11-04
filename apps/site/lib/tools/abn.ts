/** ABN checksum preflight only. Registration and GST status require ABN Lookup. */
import { validateAbn } from "@tammy/invoices/model";

export type AbnCheck = { digits: string; status: "empty" | "invalid" | "valid"; explanation: string };

export function checkAbn(value: string): AbnCheck {
  const digits = value.replace(/\s/g, "");
  if (!digits) return { digits: "", status: "empty", explanation: "Enter an 11-digit ABN to check its format." };
  if (!/^\d{11}$/.test(digits)) {
    return { digits, status: "invalid", explanation: "An ABN must contain exactly 11 digits." };
  }
  try {
    validateAbn(digits, "ABN", true);
    return {
      digits,
      status: "valid",
      explanation: "The 11-digit number passes the ABN checksum. This does not establish registration, identity or GST status.",
    };
  } catch {
    return { digits, status: "invalid", explanation: "The number fails the official ABN modulus-89 checksum." };
  }
}

export function formattedAbn(digits: string): string {
  return digits.replace(/^(\d{2})(\d{3})(\d{3})(\d{3})$/, "$1 $2 $3 $4");
}
